package types

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/qubesome/cli/internal/files"
	"gopkg.in/yaml.v3"
)

var (
	// IANA Time Zone format, not for its content.
	timezoneRegex     = regexp.MustCompile(`^[A-Za-z]+/[A-Za-z_]+$`)
	gpusRegex         = regexp.MustCompile(`^all$`)
	nameRegex         = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)
	imageRegex        = regexp.MustCompile(`^(?:(?:[a-z0-9]+(?:[._-][a-z0-9]+)*)+\/)?(?:[a-z0-9]+(?:[._-][a-z0-9]+)*)+(?:[:/][a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)
	ipRegex           = regexp.MustCompile(`^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`)
	runnerRegex       = regexp.MustCompile(`^(docker|podman|firecracker)$`)
	externalPathRegex = regexp.MustCompile(`^[a-zA-Z0-9\-]+:/[^:]+:/[^:]+$`)
	pathRegex         = regexp.MustCompile(`^(\${[a-zA-Z0-9\-]+}){0,1}/[^:]+:/[^:]+(:ro){0,1}$`)
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

	// HostAccess defines all the access request which are allowed for
	// its workloads.
	HostAccess `yaml:"hostAccess"`

	// Display holds the display to be created for this profile.
	// All workloads running within this profile will share the same
	// display.
	Display uint8 `yaml:"display"`

	// Paths defines the paths to be mounted to the profile's container.
	Paths []string `yaml:"paths"`

	// ExternalDrives defines the required external drives to run the profile.
	ExternalDrives []string `yaml:"externalDrives"`

	// Image is the container image name used for running the profile.
	// It should contain Xephyr and any additional window managers required.
	Image string

	Timezone string `yaml:"timezone"`

	DNS string `yaml:"dns"`

	// WindowManager holds the command to run the Window Manager once
	// the X server is running.
	//
	// Example: exec awesome
	WindowManager string `yaml:"windowManager"`

	// XephyrArgs defines additional args to be passed on to Xephyr.
	XephyrArgs string `yaml:"xephyrArgs"`
}

func valid(val, field string, maxLen int, allowEmpty bool, format *regexp.Regexp) error {
	if val == "" {
		if allowEmpty {
			return nil
		}
		return fmt.Errorf("%s cannot be empty", field)
	}
	if len(val) > maxLen {
		return fmt.Errorf("%s is too long: max length is %d", field, maxLen)
	}
	if format != nil && !format.MatchString(val) {
		return fmt.Errorf("%q in %s does not match format: %s", val, field, format.String())
	}
	return nil
}

func (p Profile) Validate() error {
	if err := valid(p.Name, "name", 50, false, nameRegex); err != nil {
		return err
	}
	if err := valid(p.Timezone, "timezone", 25, true, timezoneRegex); err != nil {
		return err
	}
	if err := valid(p.Image, "image", 100, true, imageRegex); err != nil {
		return err
	}
	if err := valid(p.DNS, "dns", 15, true, ipRegex); err != nil {
		return err
	}
	if err := valid(p.WindowManager, "windowManager", 50, false, nil); err != nil {
		return err
	}
	if err := valid(p.XephyrArgs, "xephyrArgs", 50, true, nil); err != nil {
		return err
	}
	if err := valid(p.Runner, "runner", 20, true, runnerRegex); err != nil {
		return err
	}
	for _, path := range p.Paths {
		if err := valid(path, "paths", 500, false, pathRegex); err != nil {
			return err
		}
	}
	for _, ed := range p.ExternalDrives {
		if err := valid(ed, "externalDrives", 500, false, externalPathRegex); err != nil {
			return err
		}
	}
	return nil
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

	cfg.RootDir = filepath.Dir(path)

	// To avoid names being defined twice on the profiles, the name
	// is only defined when referring to a profile which results
	// on the .name field of Profiles not being populated.
	for k := range cfg.Profiles {
		p := cfg.Profiles[k]
		p.Name = k
	}

	return cfg, nil
}
