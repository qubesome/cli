package config

// TODO: Maybe workload.Config instead?
type Workload struct {
	Image          string   `yaml:"image"`
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args"`
	SingleInstance bool     `yaml:"singleInstance"`
	HostAccess     `yaml:"hostAccess"`
	Paths          []string `yaml:"paths"`
	MimeApps       []string `yaml:"mimeApps"`
	NamedDevices   []string `yaml:"namedDevices"`
}

type HostAccess struct {
	X11        bool   `yaml:"x11"`
	Camera     bool   `yaml:"camera"`
	Microphone bool   `yaml:"microphone"`
	Speakers   bool   `yaml:"speakers"`
	Smartcard  bool   `yaml:"smartcard"`
	Gpu        bool   `yaml:"gpu"`
	HostName   bool   `yaml:"hostName"`
	LocalTime  bool   `yaml:"localTime"`
	Network    string `yaml:"network"`
	VarRunUser bool   `yaml:"varRunUser"`
}
