package types

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qubesome/cli/internal/env"
)

type Workload struct {
	Name           string   `yaml:"name"`
	Image          string   `yaml:"image"`
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args"`
	SingleInstance bool     `yaml:"singleInstance"`
	HostAccess     `yaml:"hostAccess"`
	MimeApps       []string `yaml:"mimeApps"`

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
	Smartcard  bool `yaml:"smartcard"`
	VarRunUser bool `yaml:"varRunUser"`
	Privileged bool `yaml:"privileged"`
	Mime       bool `yaml:"mime"`

	Bluetooth bool `yaml:"bluetooth"`

	// LocalTime defines whether the workload should share the same
	// local time as the host.
	LocalTime bool `yaml:"localTime"`

	// USBDevices defines the USB devices to be made available to a
	// workload.
	//
	// For available device names:
	// 	grep -R HID_NAME /sys/class/hidraw/*/device/uevent | cut -d'=' -f2 | sort -u
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
	}

	e.Name = fmt.Sprintf("%s-%s", w.Name, p.Name)

	e.Workload.Camera = w.Camera && p.HostAccess.Camera
	e.Workload.Smartcard = w.Smartcard && p.HostAccess.Smartcard
	e.Workload.Microphone = w.Microphone && p.HostAccess.Microphone
	e.Workload.Speakers = w.Speakers && p.HostAccess.Speakers
	e.Workload.Dbus = w.Dbus && p.HostAccess.Dbus
	e.Workload.VarRunUser = w.VarRunUser && p.HostAccess.VarRunUser
	e.Workload.LocalTime = w.LocalTime && p.HostAccess.LocalTime
	e.Workload.Bluetooth = w.Bluetooth && p.HostAccess.Bluetooth
	e.Workload.Mime = w.Mime && p.HostAccess.Mime
	e.Workload.Privileged = w.Privileged && p.HostAccess.Privileged

	// TODO: Consider restraining user on workloads.
	e.Workload.User = w.User

	if p.Gpus == "" || w.Gpus != p.Gpus {
		e.Workload.Gpus = ""
	}

	// If profile sets a network, that is enforced on all workloads.
	// If a profile does not set a network, workloads can only set "none" as a network.
	if p.Network != "" {
		e.Workload.Network = p.Network
	} else if w.Network != "" && w.Network != "none" {
		e.Workload.Network = ""
	}

	if len(p.HostAccess.Paths) == 0 {
		e.Workload.Paths = e.Workload.Paths[:0]
	} else if len(w.Paths) > 0 {
		paths := make([]string, 0, len(w.Paths))

		for _, path := range w.Paths {
			src := strings.Split(path, ":")[0]
			if pathAllowed(src, p.HostAccess.Paths) {
				paths = append(paths, path)
			}
		}

		if len(paths) == 0 {
			paths = e.Workload.Paths[:0]
		}
		e.Workload.Paths = paths
	}

	if len(p.CapsAdd) == 0 {
		e.Workload.CapsAdd = e.Workload.CapsAdd[:0]
	} else {
		caps := make([]string, 0)

		for _, cap := range w.CapsAdd {
			if slices.Contains(p.CapsAdd, cap) {
				caps = append(caps, cap)
			}
		}
		e.Workload.CapsAdd = caps
	}

	if len(p.HostAccess.Devices) == 0 {
		e.Workload.Devices = p.Devices[:0]
	} else if len(w.Devices) > 0 {
		devs := make([]string, 0, len(w.Devices))

		for _, path := range w.Devices {
			if pathAllowed(path, p.HostAccess.Devices) {
				devs = append(devs, path)
			}
		}

		if len(devs) == 0 {
			devs = e.Workload.Devices[:0]
		}
		e.Workload.Devices = devs
	}

	want := w.USBDevices
	var get []string

	for _, in := range p.USBDevices {
		for _, nd := range want {
			if in == nd {
				get = append(get, nd)
			}
		}
	}

	e.Workload.USBDevices = get

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
	return nil
}

func (w EffectiveWorkload) Validate() error {
	return nil
}
