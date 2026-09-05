package types

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qubesome/cli/internal/util/env"
)

type Workload struct {
	Name    string `yaml:"name"`
	Image   string `yaml:"image"`
	Command string `yaml:"command"`
	// Args defines X11-specific arguments.
	Args []string `yaml:"args"`
	// X11Args defines X11-specific arguments.
	X11Args []string `yaml:"x11Args"`
	// WaylandArgs defines Wayland-specific arguments.
	WaylandArgs []string `yaml:"waylandArgs"`
	// NoGPUArgs defines arguments to be used when no GPU is available.
	NoGPUArgs      []string   `yaml:"noGpuArgs"`
	SingleInstance bool       `yaml:"singleInstance"`
	HostAccess     HostAccess `yaml:"hostAccess"`
	MimeApps       []string   `yaml:"mimeApps"`

	Runner string `yaml:"runner"`
	User   *int   `yaml:"user"`
}

type HostAccess struct {
	// Dbus controls access to the dbus session running at the host.
	// If false, a new dbus session for the specific Qubesome profile
	// will be created.
	Dbus bool `yaml:"dbus"`

	// Network defines what container network the workload should be
	// bound to. If empty, uses default bridge network.
	// When set at profile level, the workload must either have the
	// same network set, or set it to 'none'.
	Network string `yaml:"network"`

	Camera     bool `yaml:"camera"`
	Microphone bool `yaml:"microphone"`
	Speakers   bool `yaml:"speakers"`
	VarRunUser bool `yaml:"varRunUser"`
	Privileged bool `yaml:"privileged"`
	Mime       bool `yaml:"mime"`

	Bluetooth bool `yaml:"bluetooth"`

	// SeccompUnconfined disables the container runtime's seccomp profile
	// for the workload, exposing the full system call surface to it.
	//
	// Some applications bring their own sandbox and need system calls that
	// the runtime's default profile denies, most notably the user namespace
	// calls used by Chromium-based browsers. Granting this gives up the
	// mitigation that stands between a workload and the host kernel, so it
	// is opt-in at both workload and profile level, exactly like privileged.
	SeccompUnconfined bool `yaml:"seccompUnconfined"`

	// USBDevices defines the USB devices to be made available to a
	// workload. Each entry is either a "vendor:product" identifier
	// (e.g. 1050:0407) matched against the device IDs, or, for backwards
	// compatibility, a prefix of the USB product name.
	//
	// To list the USB devices detected on the current machine use:
	//  qubesome usb
	USBDevices []string `yaml:"usbDevices"`
	Gpus       string   `yaml:"gpus"`
	Paths      []string `yaml:"paths"`

	CapsAdd []string `yaml:"capsAdd"`
	Devices []string `yaml:"devices"`
}

type EffectiveWorkload struct {
	// Name combines the name of both the workload and the profile
	// in which it will be executed under.
	Name     string
	Profile  *Profile
	Workload Workload
}

func (w Workload) ApplyProfile(p *Profile) EffectiveWorkload {
	e := EffectiveWorkload{
		Profile:  p,
		Workload: w,

		Name: fmt.Sprintf("%s-%s", w.Name, p.Name)}

	e.Workload.HostAccess.Camera = w.HostAccess.Camera && p.Camera
	e.Workload.HostAccess.Microphone = w.HostAccess.Microphone && p.Microphone
	e.Workload.HostAccess.Speakers = w.HostAccess.Speakers && p.Speakers
	e.Workload.HostAccess.Dbus = w.HostAccess.Dbus && p.Dbus
	e.Workload.HostAccess.VarRunUser = w.HostAccess.VarRunUser && p.VarRunUser
	e.Workload.HostAccess.Bluetooth = w.HostAccess.Bluetooth && p.Bluetooth
	e.Workload.HostAccess.Mime = w.HostAccess.Mime && p.Mime
	e.Workload.HostAccess.Privileged = w.HostAccess.Privileged && p.Privileged
	e.Workload.HostAccess.SeccompUnconfined = w.HostAccess.SeccompUnconfined && p.SeccompUnconfined

	// TODO: Consider restraining user on workloads.
	e.Workload.User = w.User

	if p.Gpus == "" || w.HostAccess.Gpus != p.Gpus {
		e.Workload.HostAccess.Gpus = ""
	}

	// If profile sets a network, that is enforced on all workloads.
	// If a profile does not set a network, workloads can only set "none" as a network.
	if p.Network != "" && w.HostAccess.Network != "none" {
		e.Workload.HostAccess.Network = p.Network
	} else if w.HostAccess.Network != "" && w.HostAccess.Network != "none" {
		e.Workload.HostAccess.Network = ""
	}

	if len(p.HostAccess.Paths) == 0 {
		e.Workload.HostAccess.Paths = e.Workload.HostAccess.Paths[:0]
	} else if len(w.HostAccess.Paths) > 0 {
		paths := make([]string, 0, len(w.HostAccess.Paths))

		for _, path := range w.HostAccess.Paths {
			src, _, _ := strings.Cut(path, ":")
			if pathAllowed(src, p.HostAccess.Paths) {
				paths = append(paths, path)
			}
		}

		if len(paths) == 0 {
			paths = e.Workload.HostAccess.Paths[:0]
		}
		e.Workload.HostAccess.Paths = paths
	}

	if len(p.CapsAdd) == 0 {
		e.Workload.HostAccess.CapsAdd = e.Workload.HostAccess.CapsAdd[:0]
	} else {
		caps := make([]string, 0)

		for _, cap := range w.HostAccess.CapsAdd {
			if slices.Contains(p.CapsAdd, cap) {
				caps = append(caps, cap)
			}
		}
		e.Workload.HostAccess.CapsAdd = caps
	}

	if len(p.Devices) == 0 {
		e.Workload.HostAccess.Devices = p.Devices[:0]
	} else if len(w.HostAccess.Devices) > 0 {
		devs := make([]string, 0, len(w.HostAccess.Devices))

		// Devices are src[:dst[:perms]], so the whole request cannot be
		// matched against the allowlist as if it were a path. Only the
		// source names a host device, and that is what the profile grants.
		for _, device := range w.HostAccess.Devices {
			src, _, _, err := ParseDevice(device)
			if err != nil {
				slog.Warn("dropping device", "device", device, "error", err)
				continue
			}

			if pathAllowed(src, p.Devices) {
				devs = append(devs, device)
			}
		}

		if len(devs) == 0 {
			devs = e.Workload.HostAccess.Devices[:0]
		}
		e.Workload.HostAccess.Devices = devs
	}

	want := w.HostAccess.USBDevices
	var get []string

	for _, in := range p.USBDevices {
		for _, nd := range want {
			if in == nd {
				get = append(get, nd)
			}
		}
	}

	e.Workload.HostAccess.USBDevices = get

	return e
}

func pathAllowed(path string, list []string) bool {
	path = filepath.Clean(env.Expand(path))
	for _, a := range list {
		a = filepath.Clean(env.Expand(a))
		if path == a {
			return true
		}
		if len(path) > len(a) &&
			strings.HasPrefix(path, a+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

func (w Workload) Validate() error {
	if err := valid(w.Name, "name", 50, false, nameRegex); err != nil {
		return err
	}
	if err := valid(w.HostAccess.Gpus, "gpus", 5, true, gpusRegex); err != nil {
		return err
	}
	if err := valid(w.Command, "command", 100, true, nil); err != nil {
		return err
	}
	if err := valid(w.Image, "image", 100, false, imageRegex); err != nil {
		return err
	}
	if err := valid(w.Runner, "runner", 20, true, runnerRegex); err != nil {
		return err
	}
	for _, mime := range w.MimeApps {
		if err := valid(mime, "mime", 100, false, nil); err != nil {
			return err
		}
	}
	// x11Args, waylandArgs and noGpuArgs are appended to the same command
	// line as args, so they are held to the same rules. The order is fixed
	// so that a workload with more than one invalid field always fails on
	// the same one.
	argFields := []struct {
		name string
		args []string
	}{
		{"args", w.Args},
		{"x11Args", w.X11Args},
		{"waylandArgs", w.WaylandArgs},
		{"noGpuArgs", w.NoGPUArgs},
	}

	for _, f := range argFields {
		for _, arg := range f.args {
			if err := valid(arg, f.name, 250, false, nil); err != nil {
				return err
			}
		}
	}
	for _, device := range w.HostAccess.Devices {
		if _, _, _, err := ParseDevice(device); err != nil {
			return err
		}
	}
	return nil
}

func (w EffectiveWorkload) Validate() error {
	if w.Profile != nil {
		err := w.Profile.Validate()
		if err != nil {
			return err
		}
	}

	return w.Workload.Validate()
}
