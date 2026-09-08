package types

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/qubesome/cli/internal/files"
)

// firecrackerRunner is the runner that backs a workload with a microVM.
const firecrackerRunner = "firecracker"

// Defaults for a microVM. They apply field by field, so a workload that
// sets only vcpus still gets the rest.
const (
	defaultVCPUs         = 2
	defaultMemoryMiB     = 1024
	defaultRootfsSizeMiB = 4096
	defaultDataSizeMiB   = 1024
)

// Bounds on the numbers a microVM is configured with.
//
// They are not a statement about the host. Asking for more memory than
// the machine has is a firecracker error at boot, and there is nothing
// here that could know better. What they catch is the value that cannot
// mean what it says: a zero rootfs is a mkfs failure with an unhelpful
// message, a zero vcpu count is a machine that cannot run, and a number
// far past any real machine is a digit typed twice.
const (
	maxVCPUs     = 32
	minMemoryMiB = 128
	maxMemoryMiB = 131072
	minDiskMiB   = 64
	maxDiskMiB   = 1048576
)

// MicroVM describes the machine a firecracker workload boots.
//
// Every field is optional, and WithDefaults fills in the ones that are
// not set, so a workload can ask for a VM by naming the runner and
// nothing else.
type MicroVM struct {
	// VCPUs is how many cores the guest sees.
	VCPUs int `yaml:"vcpus"`

	// MemoryMiB is the guest's RAM. It is a reservation taken from the
	// host for as long as the machine is up.
	MemoryMiB int `yaml:"memoryMiB"`

	// RootfsSizeMiB is the size of the ext4 built from the image. The
	// file is sparse, so this is a ceiling on what the guest may write
	// rather than what the build costs on disk.
	RootfsSizeMiB int `yaml:"rootfsSizeMiB"`

	// Data is a disk that outlives a boot. Nil means the machine has
	// only its rootfs, which is rebuilt from the image every time and
	// discarded on shutdown.
	Data *MicroVMData `yaml:"data"`
}

// MicroVMData is the disk that carries what a guest keeps.
//
// The rootfs is rebuilt from the image on every boot, so anything the
// guest writes outside this mount point is gone when it shuts down.
type MicroVMData struct {
	// Path is where the disk image lives on the host.
	Path string `yaml:"path"`

	// Mount is where the guest mounts it.
	Mount string `yaml:"mount"`

	// SizeMiB is the size the disk is created at, the first time.
	SizeMiB int `yaml:"sizeMiB"`
}

// WithDefaults returns m with every unset field filled in.
//
// A nil receiver is a workload that configured nothing, which is the
// whole set of defaults. The result never shares its Data pointer with
// the receiver, so filling a default in does not reach back into the
// configuration it was read from.
//
// The memory default is 1024 MiB and not the 256 MiB the old firecracker
// template hardcoded. That 256 was a fixed value with no configuration
// behind it, sized for the single purpose-built image that runner could
// boot. An image chosen freely is far more likely to be a development
// environment than a busybox, and 256 MiB would have it killed during
// boot with nothing to say why.
func (m *MicroVM) WithDefaults() MicroVM {
	out := MicroVM{}
	if m != nil {
		out = *m
	}

	if out.VCPUs == 0 {
		out.VCPUs = defaultVCPUs
	}
	if out.MemoryMiB == 0 {
		out.MemoryMiB = defaultMemoryMiB
	}
	if out.RootfsSizeMiB == 0 {
		out.RootfsSizeMiB = defaultRootfsSizeMiB
	}
	if out.Data != nil {
		data := *out.Data
		if data.SizeMiB == 0 {
			data.SizeMiB = defaultDataSizeMiB
		}
		out.Data = &data
	}

	return out
}

// ValidateMicroVM refuses a workload configuration a microVM cannot
// honour.
//
// It follows what ValidateDeviceRequest does for device remaps: a grant
// the runtime has no way to deliver is reported when the config is read,
// not when the user tries to open the application. A guest has its own
// kernel and no bus to the host, so most of hostAccess is not a grant
// that is merely unimplemented here. There is nowhere for it to go.
//
// Two of these will look surprising. singleInstance must be true, which
// inverts the bwrap runner's rule, because a machine is a boot and a
// memory reservation and there is one of it per workload rather than one
// per launch. And usbDevices is refused rather than dropped, because
// firecracker has no USB support at all: a workload granted a security
// key that silently did not get one is worse than a workload that does
// not start.
func ValidateMicroVM(w Workload) error {
	// attachVM becomes a filename, so it is held to the same alphabet as
	// every other name that does. Whether it names a workload that
	// exists, and one that is a firecracker workload, cannot be answered
	// here: a workload file is validated on its own and the target lives
	// in another file. internal/qubesome/run.go answers it at launch.
	if w.AttachVM != "" {
		if err := files.ValidateName("attachVM", w.AttachVM); err != nil {
			return err
		}
	}

	if w.Runner != firecrackerRunner {
		if w.MicroVM != nil {
			return fmt.Errorf("microvm is set on a workload whose runner is not firecracker: the block configures a machine that will not exist")
		}
		return nil
	}

	if w.AttachVM != "" {
		return fmt.Errorf("attachVM cannot be set on a firecracker workload: a microVM does not attach to a microVM")
	}

	if !w.SingleInstance {
		return fmt.Errorf("singleInstance must be true on a firecracker workload: a machine is a boot and a memory reservation, and there is one per workload rather than one per launch")
	}

	// The order is fixed so that a workload with more than one refused
	// field always fails on the same one.
	grants := []struct {
		field   string
		granted bool
		reason  string
	}{
		{"gpus", w.HostAccess.Gpus != "", "there is no PCI bus in a guest and pci=off is on its kernel command line"},
		{"camera", w.HostAccess.Camera, "a host device node cannot be bound into a guest"},
		{"microphone", w.HostAccess.Microphone, "a host device node cannot be bound into a guest"},
		{"speakers", w.HostAccess.Speakers, "a host device node cannot be bound into a guest"},
		{"dbus", w.HostAccess.Dbus, "a host socket cannot be bound into a guest"},
		{"bluetooth", w.HostAccess.Bluetooth, "a host socket cannot be bound into a guest"},
		{"varRunUser", w.HostAccess.VarRunUser, "a host socket cannot be bound into a guest"},
		{"mime", w.HostAccess.Mime, "mime handling needs the qubesome socket and the profile's mTLS values, and neither crosses into a guest"},
	}

	for _, g := range grants {
		if g.granted {
			return fmt.Errorf("%s cannot be granted to a firecracker workload: %s", g.field, g.reason)
		}
	}

	if len(w.HostAccess.USBDevices) > 0 {
		return fmt.Errorf("usbDevices cannot be granted to a firecracker workload: firecracker has no USB controller and no PCI bus, so the device would silently not be there")
	}
	if len(w.HostAccess.Devices) > 0 {
		return fmt.Errorf("devices cannot be granted to a firecracker workload: a host device node cannot be bound into a guest")
	}
	if len(w.HostAccess.CapsAdd) > 0 {
		return fmt.Errorf("capsAdd cannot be granted to a firecracker workload: capabilities are the guest kernel's, and there is nothing on the host to grant")
	}
	if w.User != nil {
		return fmt.Errorf("user cannot be set on a firecracker workload: the rootfs is built through a single uid mapping, so only uid 0 can be expressed")
	}

	// A path is composed into the image before the machine boots, so a
	// write reaches a copy that is discarded on shutdown. There is no
	// shared host directory to write through.
	for _, p := range w.HostAccess.Paths {
		if !strings.HasSuffix(p, ":ro") {
			return fmt.Errorf("path %q cannot be granted read-write to a firecracker workload: it is composed into the rootfs before boot, so a write would go to a copy that is discarded", p)
		}
	}

	return validateMicroVMSizes(w.MicroVM.WithDefaults())
}

// validateMicroVMSizes bounds the numbers, after the defaults are in, so
// an unset field is checked against the value it will actually boot with.
func validateMicroVMSizes(m MicroVM) error {
	if m.VCPUs < 1 || m.VCPUs > maxVCPUs {
		return fmt.Errorf("microvm vcpus %d is out of range: must be between 1 and %d", m.VCPUs, maxVCPUs)
	}
	if m.MemoryMiB < minMemoryMiB || m.MemoryMiB > maxMemoryMiB {
		return fmt.Errorf("microvm memoryMiB %d is out of range: must be between %d and %d",
			m.MemoryMiB, minMemoryMiB, maxMemoryMiB)
	}
	if m.RootfsSizeMiB < minDiskMiB || m.RootfsSizeMiB > maxDiskMiB {
		return fmt.Errorf("microvm rootfsSizeMiB %d is out of range: must be between %d and %d",
			m.RootfsSizeMiB, minDiskMiB, maxDiskMiB)
	}

	if m.Data == nil {
		return nil
	}

	if err := valid(m.Data.Path, "microvm data path", 500, false, microvmPathRegex); err != nil {
		return err
	}
	if err := valid(m.Data.Mount, "microvm data mount", 500, false, nil); err != nil {
		return err
	}
	// The mount point is a path in the guest, so it carries none of the
	// host variables the data path may, and it is where the guest's own
	// mount call is pointed.
	if !filepath.IsAbs(m.Data.Mount) || m.Data.Mount != filepath.Clean(m.Data.Mount) {
		return fmt.Errorf("microvm data mount %q must be an absolute, clean path", m.Data.Mount)
	}
	if m.Data.SizeMiB < minDiskMiB || m.Data.SizeMiB > maxDiskMiB {
		return fmt.Errorf("microvm data sizeMiB %d is out of range: must be between %d and %d",
			m.Data.SizeMiB, minDiskMiB, maxDiskMiB)
	}

	return nil
}

// WarnIgnoredMicroVMFields reports the fields of a firecracker workload
// that have no effect.
//
// There is no X server in a guest, so the arguments that exist to
// configure one, and the mime registrations that exist to reach one, are
// read by nothing. They are warned about rather than refused because they
// are inert: a workload carrying them still does exactly what it was
// asked to do, and refusing would break a config that only ever shared
// these fields with its non-VM sibling.
//
// Callers invoke this once per launch, for the same reason
// WarnIgnoredNetwork says: Validate runs several times for a single
// launch and would otherwise warn repeatedly.
func WarnIgnoredMicroVMFields(name string, w Workload) {
	if w.Runner != firecrackerRunner {
		return
	}

	for _, f := range []struct {
		field string
		set   bool
	}{
		{"x11Args", len(w.X11Args) > 0},
		{"noGpuArgs", len(w.NoGPUArgs) > 0},
		{"mimeApps", len(w.MimeApps) > 0},
	} {
		if f.set {
			slog.Warn("field is ignored on a firecracker workload, a guest has no X server",
				"name", name, "field", f.field)
		}
	}
}
