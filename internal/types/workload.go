package types

import "fmt"

type Workload struct {
	Name           string   `yaml:"name"`
	Image          string   `yaml:"image"`
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args"`
	SingleInstance bool     `yaml:"singleInstance"`
	HostAccess     `yaml:"hostAccess"`
	Paths          []string `yaml:"paths"`
	HomePaths      []string `yaml:"homePaths"`
	Volumes        []string `yaml:"volumes"`
	MimeApps       []string `yaml:"mimeApps"`

	// TODO: Rename to USB Named Devices
	// grep -R HID_NAME /sys/class/hidraw/*/device/uevent | cut -d'=' -f2 | sort -u
	NamedDevices []string `yaml:"namedDevices"`
	Runner       string   `yaml:"runner"`
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

	want := w.NamedDevices
	var get []string

	for _, in := range p.NamedDevices {
		for _, nd := range want {
			if in == nd {
				get = append(get, nd)
			}
		}
	}

	e.Workload.NamedDevices = get

	return e
}

func (w Workload) Validate() error {
	return nil
}

func (w EffectiveWorkload) Validate() error {
	return nil
}
