package config

type Config struct {
	// MimeHandler configures mime types and the specific workloads to handle them.
	MimeHandlers map[string]MimeHandler `yaml:"mimeHandlers"`

	DefaultMimeHandler *MimeHandler `yaml:"defaultMimeHandler"`
}

type MimeHandler struct {
	Workload string `yaml:"workload"`
	Profile  string `yaml:"profile"`
}
