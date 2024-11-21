package usb

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func NamedDevices(names []string) ([]string, error) {
	devs := []string{}

	products, err := filepath.Glob("/sys/bus/usb/devices/*/product")
	if err != nil {
		return nil, fmt.Errorf("failed to get USB device files: %w", err)
	}

	for _, fn := range products {
		d, err := readFile(fn, names)
		if err != nil {
			return nil, fmt.Errorf("failed to get USB device files: %w", err)
		}
		devs = append(devs, d...)
	}

	return devs, nil
}

func readFile(fn string, names []string) ([]string, error) {
	devs := []string{}

	f, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to open USB device file: %w", err)
	}

	r := bufio.NewScanner(f)
	for r.Scan() {
		devName := r.Text()

		for _, n := range names {
			if strings.HasPrefix(devName, n) {
				parent := filepath.Dir(fn)

				busNum, err := getValue(filepath.Join(parent, "busnum"))
				if err != nil {
					return nil, err
				}
				devNum, err := getValue(filepath.Join(parent, "devnum"))
				if err != nil {
					return nil, err
				}

				devs = append(devs, fmt.Sprintf("/dev/bus/usb/%03d/%03d", busNum, devNum))

				// More on USB and /sys/bus/usb
				// https://www.makelinux.net/ldd3/chp-13-sect-2.shtml
				//
				// Some devices will have multiple hidraw files, such as YubiKeys:
				// /sys/bus/usb/devices/5-2.2.3/5-2.2.3:1.0/*/hidraw/hidraw9
				// /sys/bus/usb/devices/5-2.2.3/5-2.2.3:1.1/*/hidraw/hidraw10
				hidfiles, err := filepath.Glob(filepath.Join(parent, fmt.Sprintf("%s:*", filepath.Base(parent)), "*", "hidraw", "hidraw*"))
				if err != nil {
					return nil, fmt.Errorf("failed to Glob for hidraw files: %w", err)
				}
				if len(hidfiles) == 0 {
					slog.Debug("no hidraw files found", "device", n)
				}
				for _, hid := range hidfiles {
					devs = append(devs, fmt.Sprintf("/dev/%s", filepath.Base(hid)))
				}
			}
		}
	}

	return devs, nil
}

func getValue(fn string) (int, error) {
	bn, err := os.ReadFile(fn)
	if err != nil {
		return 0, fmt.Errorf("failed to read USB file: %w", err)
	}

	n, err := strconv.Atoi(string(bytes.TrimSpace(bn)))
	if err != nil {
		return 0, fmt.Errorf("failed to convert %s to int: %w", bn, err)
	}
	return n, nil
}
