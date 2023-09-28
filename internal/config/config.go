package config

type Config struct {
	Logging Logging `yaml:"logging"`

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
