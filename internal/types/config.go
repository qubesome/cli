package types

type Config struct {
	Logging Logging `yaml:"logging"`

	Profiles map[string]Profile `yaml:"profiles"`

	// MimeHandler configures mime types and the specific workloads to handle them.
	MimeHandlers map[string]MimeHandler `yaml:"mimeHandlers"`

	DefaultMimeHandler *MimeHandler `yaml:"defaultMimeHandler"`
}

type Logging struct {
	LogToFile bool   `yaml:"logToFile"`
	Level     string `yaml:"level"`
}

type MimeHandler struct {
	Workload string `yaml:"workload"`
	Profile  string `yaml:"profile"`
}

type Profile struct {
	Name   string
	Path   string
	Runner string // TODO: Better name runner

	HostAccess `yaml:"hostAccess"`

	// TODO: Rename to USB named devices
	NamedDevices []string `yaml:"namedDevices"`
}
