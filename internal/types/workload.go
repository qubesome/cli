package types

import (
	"fmt"
	"path/filepath"
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
}

type HostAccess struct {
	X11        bool   `yaml:"x11"`
	Camera     bool   `yaml:"camera"`
	Microphone bool   `yaml:"microphone"`
	Speakers   bool   `yaml:"speakers"`
	Smartcard  bool   `yaml:"smartcard"`
	Network    string `yaml:"network"`
	VarRunUser bool   `yaml:"varRunUser"`
	Privileged bool   `yaml:"privileged"`
	Mime       bool   `yaml:"mime"`

	Bluetooth bool `yaml:"bluetooth"`

	// MachineID defines whether the workload should share the same
	// machine id as the host.
	MachineID bool `yaml:"machineId"`

	// LocalTime defines whether the workload should share the same
	// local time as the host.
	LocalTime bool `yaml:"localTime"`

	// USBDevices defines the USB devices to be made available to a
	// container.
	//
	// For available device names:
	// 	grep -R HID_NAME /sys/class/hidraw/*/device/uevent | cut -d'=' -f2 | sort -u
	USBDevices []string `yaml:"usbDevices"`
	Gpus       string   `yaml:"gpus"`
	Paths      []string `yaml:"paths"`
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
	e.Workload.X11 = w.X11 && p.HostAccess.X11
	e.Workload.VarRunUser = w.VarRunUser && p.HostAccess.VarRunUser
	e.Workload.MachineID = w.MachineID && p.HostAccess.MachineID
	e.Workload.LocalTime = w.LocalTime && p.HostAccess.LocalTime
	e.Workload.Bluetooth = w.Bluetooth && p.HostAccess.Bluetooth
	e.Workload.Mime = w.Mime && p.HostAccess.Mime
	e.Workload.Privileged = w.Privileged && p.HostAccess.Privileged

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
			if pathAllowed(src, p) {
				paths = append(paths, path)
			}
		}

		if len(paths) == 0 {
			paths = e.Workload.Paths[:0]
		}
		e.Workload.Paths = paths
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

func pathAllowed(path string, p *Profile) bool {
	path = filepath.Clean(env.Expand(path))
	for _, a := range p.HostAccess.Paths {
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
