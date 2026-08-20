package usb

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/qubesome/libudev"
	"github.com/qubesome/libudev/types"
)

const (
	sysDevicesDir = "/sys/devices"
	udevDataDir   = "/run/udev/data"
)

// vendorProductRE matches a USB "vendor:product" identifier, e.g. 1050:0407.
var vendorProductRE = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{4}$`)

// hidrawRE matches a hidraw device node name, e.g. hidraw9.
var hidrawRE = regexp.MustCompile(`^hidraw[0-9]+$`)

// Info describes a USB device detected on the host.
type Info struct {
	VendorID  string
	ProductID string
	Product   string
	Paths     []string
}

// NamedDevices returns the /dev paths for the USB devices selected by names.
//
// Each name is either a "vendor:product" identifier (e.g. 1050:0407) matched
// against the device IDs, or, for backwards compatibility, a prefix of the USB
// product name.
func NamedDevices(names []string) ([]string, error) {
	devices, err := scan()
	if err != nil {
		return nil, err
	}

	return selectDevices(devices, names)
}

// List returns the USB devices detected on the host.
func List() ([]Info, error) {
	devices, err := scan()
	if err != nil {
		return nil, err
	}

	return listDevices(devices), nil
}

func scan() ([]*types.Device, error) {
	devicesRoot, err := os.OpenRoot(sysDevicesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", sysDevicesDir, err)
	}
	defer devicesRoot.Close()

	// The udev runtime data enriches devices with tags. This tool does not
	// need it, and /run/udev/data may not exist. Fall back to an empty
	// directory so the scanner does not fail when it is absent.
	udevRoot, cleanup, err := openUdevDataRoot()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	s, err := libudev.NewScanner(
		libudev.WithDevicesRoot(devicesRoot),
		libudev.WithUDevDataRoot(udevRoot),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create udev scanner: %w", err)
	}

	devices, err := s.ScanDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to scan udev devices: %w", err)
	}

	return devices, nil
}

func openUdevDataRoot() (*os.Root, func(), error) {
	root, err := os.OpenRoot(udevDataDir)
	if err == nil {
		return root, func() { _ = root.Close() }, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("failed to open %s: %w", udevDataDir, err)
	}

	tmp, err := os.MkdirTemp("", "qubesome-udev-*")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp udev data dir: %w", err)
	}
	root, err = os.OpenRoot(tmp)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return nil, nil, fmt.Errorf("failed to open temp udev data dir: %w", err)
	}

	return root, func() {
		_ = root.Close()
		_ = os.RemoveAll(tmp)
	}, nil
}

func selectDevices(devices []*types.Device, names []string) ([]string, error) {
	var devs []string

	for _, d := range devices {
		if !isUSBDevice(d) {
			continue
		}
		if !matchesAny(d, names) {
			continue
		}

		paths, err := devicePaths(d)
		if err != nil {
			return nil, err
		}
		devs = append(devs, paths...)
	}

	return devs, nil
}

func listDevices(devices []*types.Device) []Info {
	var infos []Info

	for _, d := range devices {
		if !isUSBDevice(d) {
			continue
		}

		info := Info{
			VendorID:  d.VendorID,
			ProductID: d.ProductID,
			Product:   d.Attrs["product"],
		}
		if paths, err := devicePaths(d); err == nil {
			info.Paths = paths
		} else {
			info.Paths = []string{fmt.Sprintf("error: %v", err)}
		}
		infos = append(infos, info)
	}

	sort.Slice(infos, func(i, j int) bool {
		if infos[i].VendorID != infos[j].VendorID {
			return infos[i].VendorID < infos[j].VendorID
		}
		return infos[i].ProductID < infos[j].ProductID
	})

	return infos
}

// isUSBDevice reports whether d is a top-level USB device node. Only those nodes
// carry busnum and devnum attributes. Interfaces and hidraw children inherit the
// vendor and product IDs but not these, so this avoids duplicate matches.
func isUSBDevice(d *types.Device) bool {
	return d.Attrs["busnum"] != "" && d.Attrs["devnum"] != ""
}

func matchesAny(d *types.Device, names []string) bool {
	for _, n := range names {
		if vendor, product, ok := parseVendorProduct(n); ok {
			if strings.EqualFold(d.VendorID, vendor) && strings.EqualFold(d.ProductID, product) {
				return true
			}
			continue
		}

		if strings.HasPrefix(d.Attrs["product"], n) {
			return true
		}
	}

	return false
}

func devicePaths(d *types.Device) ([]string, error) {
	busNum, err := strconv.Atoi(d.Attrs["busnum"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse busnum %q: %w", d.Attrs["busnum"], err)
	}
	devNum, err := strconv.Atoi(d.Attrs["devnum"])
	if err != nil {
		return nil, fmt.Errorf("failed to parse devnum %q: %w", d.Attrs["devnum"], err)
	}

	// Some USB devices, such as YubiKeys, have multiple hidraw nodes nested
	// under their interfaces. Include all of them so tools relying on hidraw
	// (e.g. FIDO/SK keys) work inside the container.
	hids := hidrawNodes(d)
	if len(hids) == 0 {
		slog.Debug("no hidraw files found", "device", d.Attrs["product"])
	}

	paths := make([]string, 0, 1+len(hids))
	paths = append(paths, fmt.Sprintf("/dev/bus/usb/%03d/%03d", busNum, devNum))
	paths = append(paths, hids...)

	return paths, nil
}

// hidrawNodes collects the /dev paths of the hidraw nodes belonging to d. It
// descends through d's interfaces but stops at downstream USB devices (e.g. a
// hub's connected devices), which own their hidraw nodes and are handled on
// their own.
func hidrawNodes(d *types.Device) []string {
	var hids []string

	for _, c := range d.Children {
		if isUSBDevice(c) {
			continue
		}
		if hidrawRE.MatchString(filepath.Base(c.Devpath)) {
			hids = append(hids, "/dev/"+filepath.Base(c.Devpath))
		}
		hids = append(hids, hidrawNodes(c)...)
	}

	return hids
}

func parseVendorProduct(s string) (vendor, product string, ok bool) {
	if !vendorProductRE.MatchString(s) {
		return "", "", false
	}

	v, p, _ := strings.Cut(s, ":")
	return v, p, true
}
