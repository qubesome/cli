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
	MimeApps       []string `yaml:"mimeApps"`

	// TODO: Rename to USB Named Devices
	// grep -R HID_NAME /sys/class/hidraw/*/device/uevent | cut -d'=' -f2
	NamedDevices []string `yaml:"namedDevices"`
	Runner       string   `yaml:"runner"`
}

type HostAccess struct {
	X11        bool   `yaml:"x11"`
	Camera     bool   `yaml:"camera"`
	Microphone bool   `yaml:"microphone"`
	Speakers   bool   `yaml:"speakers"`
	Smartcard  bool   `yaml:"smartcard"`
	HostName   bool   `yaml:"hostName"`
	Network    string `yaml:"network"`
	VarRunUser bool   `yaml:"varRunUser"`
}

type EffectiveWorkload struct {
	// Name combines the name of both the workload and the profile
	// in which it will be executed under.
	Name     string
	Profile  Profile
	Workload Workload
}

func (w Workload) ApplyProfile(p Profile) EffectiveWorkload {
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
	e.Workload.HostName = w.HostName && p.HostAccess.HostName

	return e
}

func (w Workload) Validate() error {
	return nil
}

func (w EffectiveWorkload) Validate() error {
	return nil
}
