package types

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/qubesome/cli/internal/files"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Logging Logging `yaml:"logging"`

	Profiles map[string]*Profile `yaml:"profiles"`

	// MimeHandler configures mime types and the specific workloads to handle them.
	MimeHandlers map[string]MimeHandler `yaml:"mimeHandlers"`

	DefaultMimeHandler *MimeHandler `yaml:"defaultMimeHandler"`

	// WorkloadPullMode defines how workload images should be pulled.
	WorkloadPullMode WorkloadPullMode `yaml:"workloadPullMode"`

	RootDir string
}

// WorkloadFiles returns a list of workload file paths.
func (c *Config) WorkloadFiles() ([]string, error) {
	var matches []string
	root := c.RootDir
	if root == "" {
		root = files.QubesomeConfig()
	}
	pattern := fmt.Sprintf("^%s/.*/workloads/.*.yaml$", root)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		matched, err := regexp.MatchString(pattern, path)
		if err != nil {
			return err
		}
		if matched {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}

type Logging struct {
	LogToFile   bool   `yaml:"logToFile"`
	LogToStdout bool   `yaml:"logToStdout"`
	LogToSyslog bool   `yaml:"logToSyslog"`
	Level       string `yaml:"level"`
}

type MimeHandler struct {
	Workload string `yaml:"workload"`
	Profile  string `yaml:"profile"`
}

type Profile struct {
	Name string
	// Path defines the root path for the given profile. All other
	// paths (e.g. Paths) will descend from it.
	//
	// Note that this Path descends from the dir where the qubesome
	// config is being consumed. When sourcing from git, it descends
	// from the git repository directory.
	Path   string
	Runner string // TODO: Better name runner

	HostAccess `yaml:"hostAccess"`

	// TODO: Rename to USB named devices
	NamedDevices []string `yaml:"namedDevices"`

	Display uint8 `yaml:"display"`

	Paths          []string `yaml:"paths"`
	ExternalDrives []string `yaml:"externalDrives"`

	WindowManager string `yaml:"windowManager"`
}

func LoadConfig(path string) (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(data, cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal qubesome config %q: %w", path, err)
	}

	// To avoid names being defined twice on the profiles, the name
	// is only defined when referring to a profile which results
	// on the .name field of Profiles not being populated.
	for k := range cfg.Profiles {
		p := cfg.Profiles[k]
		p.Name = k
	}

	return cfg, nil
}
