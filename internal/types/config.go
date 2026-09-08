package types

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/qubesome/cli/internal/files"
	"go.yaml.in/yaml/v3"
)

var (
	// IANA Time Zone format, not for its content.
	timezoneRegex     = regexp.MustCompile(`^[A-Za-z]+/[A-Za-z_]+$`)
	gpusRegex         = regexp.MustCompile(`^all$`)
	nameRegex         = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)
	imageRegex        = regexp.MustCompile(`^(?:(?:[a-z0-9]+(?:[._-][a-z0-9]+)*)+\/)?(?:[a-z0-9]+(?:[._-][a-z0-9]+)*)+(?:[:/][a-z0-9]+(?:[._-][a-z0-9]+)*)+$`)
	ipRegex           = regexp.MustCompile(`^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$`)
	runnerRegex       = regexp.MustCompile(`^firecracker$`)
	externalPathRegex = regexp.MustCompile(`^[a-zA-Z0-9\-]+:/[^:]+:/[^:]+$`)
	pathRegex         = regexp.MustCompile(`^(\${[a-zA-Z0-9\-]+}){0,1}/[^:]+:/[^:]+(:ro){0,1}$`)
	// A microVM data disk names one host path rather than a mapping, so
	// it is pathRegex without the destination half. The optional leading
	// variable is kept, because that is how the reference configuration
	// writes a path into the qubesome data directory.
	microvmPathRegex = regexp.MustCompile(`^(\${[a-zA-Z0-9\-]+}){0,1}/[^:]+$`)
	// Flatpak application IDs are dot-separated elements of alphanumerics,
	// underscores and hyphens, where no element starts with a digit.
	// Uppercase is common (e.g. org.freedesktop.Bustle).
	flatpakRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(\.[A-Za-z_][A-Za-z0-9_-]*)+$`)
	// Device paths are always absolute and always under /dev. The bare
	// /dev is excluded on purpose: sharing the whole device tree is not a
	// device request.
	devicePathRegex  = regexp.MustCompile(`^/dev(/[a-zA-Z0-9_.\-]+)+$`)
	devicePermsRegex = regexp.MustCompile(`^[rwm]{1,3}$`)
)

type Config struct {
	Logging Logging `yaml:"logging"`

	Profiles map[string]Profile `yaml:"profiles"`

	// MimeHandler configures mime types and the specific workloads to handle them.
	MimeHandlers map[string]MimeHandler `yaml:"mimeHandlers"`

	DefaultMimeHandler *MimeHandler `yaml:"defaultMimeHandler"`

	// WorkloadPullMode defines how workload images should be pulled.
	WorkloadPullMode WorkloadPullMode `yaml:"workloadPullMode"`

	// Gateway describes the qubesome gateway for this configuration.
	//
	// A nil block means there is no gateway, and therefore no egress for
	// any workload. That is the behaviour of a sandbox without one and it
	// has to stay reachable, so the absence of the block is a valid
	// configuration rather than a missing one.
	Gateway *GatewayConfig `yaml:"gateway"`

	RootDir string
}

// GatewayConfig describes the gateway that gives workloads their egress.
//
// There is one gateway per session rather than one per profile, because
// the policy file it applies is keyed by workload across every profile.
type GatewayConfig struct {
	// Image is the container image the gateway runs from.
	Image string `yaml:"image"`

	// Config is the gateway's own policy file, written as a path into
	// the qubesome config tree so that it travels with the repository
	// the configuration is kept in. Resolve it with ConfigPath.
	Config string `yaml:"config"`

	// Subnet is the address range workloads are given on the link to the
	// gateway. It has to be IPv4: the gateway recovers the original
	// destination of a redirected flow through an IPv4 only path, so a
	// flow over IPv6 could never be matched against a policy.
	Subnet string `yaml:"subnet"`
}

// ConfigPath returns the gateway's policy file resolved against root,
// the directory the qubesome config was read from.
//
// files.JoinProfilePath and not filepath.Join, for the reason its doc
// comment spells out: a path in a qubesome config is written rooted at
// the config tree, so "/gateway.yml" names the tree's own file and not
// one at the root of the disk. filepath.Join would hand back "/gateway.yml"
// unchanged for the spelling every real configuration uses, and would let
// a path built from ".." leave the tree for the other spelling.
func (g GatewayConfig) ConfigPath(root string) (string, error) {
	return files.JoinProfilePath(root, g.Config)
}

// SubnetPrefix parses the subnet the gateway hands addresses out of.
//
// A prefix longer than /30 is refused because it has no host addresses
// left once the network and the broadcast address are taken, so there is
// nothing to give a workload. A prefix carrying host bits, 10.111.0.5/24,
// is refused too: it names a host where a network was meant, and the
// only reason to write one is a mistake.
func (g GatewayConfig) SubnetPrefix() (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(g.Subnet)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid gateway subnet: %w", err)
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("invalid gateway subnet %q: must be IPv4", g.Subnet)
	}
	if prefix.Bits() > 30 {
		return netip.Prefix{}, fmt.Errorf("invalid gateway subnet %q: has no host addresses", g.Subnet)
	}
	if prefix.Masked() != prefix {
		return netip.Prefix{}, fmt.Errorf("invalid gateway subnet %q: names a host, not a network", g.Subnet)
	}

	return prefix, nil
}

// Validate checks the gateway block.
//
// root is the directory the config was read from. The policy file path is
// resolved here so that one which leaves the config tree is reported when
// the config is read rather than when the gateway is started. Resolving
// is path arithmetic and reads nothing, so validation still does not
// touch the filesystem.
func (g GatewayConfig) Validate(root string) error {
	if err := valid(g.Image, "gateway image", 100, false, imageRegex); err != nil {
		return err
	}
	if err := valid(g.Config, "gateway config", 200, false, nil); err != nil {
		return err
	}
	if _, err := g.ConfigPath(root); err != nil {
		return err
	}
	if _, err := g.SubnetPrefix(); err != nil {
		return err
	}

	return nil
}

func (c *Config) Profile(name string) (*Profile, bool) {
	if c == nil || len(c.Profiles) == 0 {
		return nil, false
	}
	p, ok := c.Profiles[name]
	return &p, ok
}

// WorkloadFiles returns a list of workload file paths.
func (c *Config) WorkloadFiles() ([]string, error) {
	var matches []string
	root := c.RootDir
	slog.Debug("workload files lookup", "root", root)

	for _, profile := range c.Profiles {
		if c.RootDir == files.RunUserQubesome() {
			ln := filepath.Join(files.RunUserQubesome(), profile.Name+".config")
			target, err := os.Readlink(ln)
			if err != nil {
				slog.Debug("fail to Readlink", "err", err)
				continue
			}
			root = filepath.Dir(target)
		}

		wd := filepath.Join(root, profile.Name, "workloads")
		we, err := os.ReadDir(wd)
		if err != nil {
			slog.Debug("fail to ReadDir", "err", err, "wd", wd)
			continue
		}

		for _, w := range we {
			if w.IsDir() {
				continue
			}

			path := filepath.Join(wd, w.Name())
			if filepath.Ext(w.Name()) == ".yaml" {
				matches = append(matches, path)
			}
		}
	}

	return matches, nil
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

// Profile is the isolation boundary.
//
// Its workloads share one X display, so any of them can read another's
// window contents, observe its keystrokes and read its selections. The
// per-workload hostAccess grants govern what each workload reaches on the
// host, which stays meaningful, but they do not make workloads private
// from each other. Anything that needs to be unobservable by another
// application belongs in its own profile.
type Profile struct {
	Name string
	// Path defines the root path for the given profile. All other
	// paths (e.g. Paths) will descend from it.
	//
	// Note that this Path descends from the dir where the qubesome
	// config is being consumed. When sourcing from git, it descends
	// from the git repository directory.
	Path   string `yaml:"path"`
	Runner string `yaml:"runner"`

	// HostAccess defines all the access request which are allowed for
	// its workloads.
	HostAccess `yaml:"hostAccess"`

	// Display holds the display to be created for this profile.
	// All workloads running within this profile will share the same
	// display.
	Display uint8 `yaml:"display"`

	// Flatpaks defines the Flatpak applications from Host to be made available
	// to the profile.
	Flatpaks []string `yaml:"flatpaks"`

	// Paths defines the paths to be mounted to the profile's container.
	Paths []string `yaml:"paths"`

	// ExternalDrives defines the required external drives to run the profile.
	ExternalDrives []string `yaml:"externalDrives"`

	// Image is the container image name used for running the profile.
	//
	// It must provide weston at /usr/bin/weston and xwayland-run at
	// /usr/bin/xwayland-run, which the profile's entrypoint runs by
	// absolute path, along with any window managers the profile uses. An
	// image missing either starts and exits immediately, reported as the
	// profile exiting before it was ready.
	Image string `yaml:"image"`

	Timezone string `yaml:"timezone"`

	DNS string `yaml:"dns"`

	// WindowManager holds the command to run the Window Manager once
	// the X server is running. It runs as the X server's only client, and
	// is split into arguments without a shell, so shell syntax in it is
	// not interpreted.
	//
	// Example: exec awesome
	WindowManager string `yaml:"windowManager"`

	// Fullscreen makes the profile fill a host screen instead of being a
	// window the host window manager places.
	//
	// It does not grab input. Host window manager shortcuts still take
	// precedence over the profile, whether it is fullscreen or not.
	Fullscreen bool `yaml:"fullscreen"`

	// XephyrArgs defines additional args to be passed on to the profile's
	// X server.
	//
	// The name is kept for compatibility. Profiles used to run Xephyr, and
	// renaming the field would break every existing dotfiles repository.
	// The arguments now reach Xwayland, which accepts the same X server
	// options, so a Xephyr specific flag configured here will no longer
	// have an effect.
	XephyrArgs string `yaml:"xephyrArgs"`
}

// removedRunners are the runners qubesome used to have. They are named
// so that a config still asking for one is told it was removed, rather
// than being handed the format error a typo gets, which reads as if the
// runner never existed.
var removedRunners = []string{"docker", "podman"}

// validateRunner checks the runner a profile or a workload asks for.
//
// The wording matches what internal/qubesome/run.go refuses a launch with
// and what internal/doctor reports, so the same config reads the same way
// whether it is loaded, diagnosed or run.
func validateRunner(runner string) error {
	if slices.Contains(removedRunners, runner) {
		return fmt.Errorf("the %q runner has been removed: workloads run under bwrap", runner)
	}

	return valid(runner, "runner", 20, true, runnerRegex)
}

// WarnIgnoredNetwork reports a hostAccess.network value that has no
// effect.
//
// An empty value, none and host all mean something to a sandbox. Any
// other value names a network that only the qubesome gateway can create,
// and until that lands the sandbox gets its own namespace with loopback
// and nothing else. The name is kept rather than refused because the
// reference configuration is full of them and the gateway gives them
// meaning again.
//
// Callers invoke this once per launch. Validate runs several times for a
// single launch, so the same config would otherwise warn repeatedly.
func WarnIgnoredNetwork(name, network string) {
	switch network {
	case "", "none", "host":
		return
	}

	slog.Warn("hostAccess.network is ignored until the qubesome gateway lands, the sandbox gets loopback only",
		"name", name, "network", network)
}

// WarnIgnoredDNS reports a profile dns value that has no effect.
//
// The container runners passed it as --dns. A sandbox has no resolver to
// point anywhere, and no network to resolve against, so the value is read
// by nothing. It is kept for the same reason a named network is: name
// resolution is the gateway's, and this is the field that would configure
// it.
func WarnIgnoredDNS(name, dns string) {
	if dns == "" {
		return
	}

	slog.Warn("profile dns is ignored until the qubesome gateway lands, the sandbox has no resolver",
		"name", name, "dns", dns)
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
	if err := validateRunner(p.Runner); err != nil {
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
	// Flatpak names are used to build file paths and are interpolated into
	// the Exec line of the generated desktop files.
	for _, fp := range p.Flatpaks {
		if err := ValidateFlatpakName(fp); err != nil {
			return err
		}
	}
	// A profile only ever grants a source device, never a mapping.
	for _, device := range p.Devices {
		if err := ValidateDeviceGrant(device); err != nil {
			return err
		}
	}
	return nil
}

// ValidateFlatpakName reports whether name is a valid Flatpak application ID.
func ValidateFlatpakName(name string) error {
	if name == "" {
		return fmt.Errorf("missing flatpak name")
	}
	if len(name) > 100 {
		return fmt.Errorf("invalid flatpak name: %q is too long", name)
	}
	if !flatpakRegex.MatchString(name) {
		return fmt.Errorf("invalid flatpak name: %q", name)
	}
	return nil
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return DecodeConfig(f, path)
}

// DecodeConfig reads a config from r.
//
// path is where r was opened from. It roots the relative paths the config
// declares and names the config in errors, so a caller that had to open
// the file some other way still gets both. Callers that must prove the
// file is inside a directory open it through an os.Root and hand the
// result here, rather than passing a path back to LoadConfig and having
// it resolved a second time.
func DecodeConfig(r io.Reader, path string) (*Config, error) {
	cfg := &Config{}

	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true) // Enforces that all YAML fields match struct fields exactly.
	// A partially decoded config must never be used. Every hostAccess
	// restriction is deny-by-default, so returning what was decoded before
	// the error would silently drop restrictions.
	// cfg is already a pointer. Decoding into &cfg would hand yaml a
	// **Config, which it follows by replacing the pointer rather than
	// filling the struct, so a document that is an explicit null sets cfg
	// to nil without reporting an error.
	if err := decoder.Decode(cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("config %q is empty", path)
		}
		return nil, fmt.Errorf("failed to decode config %q: %w", path, err)
	}

	cfg.RootDir = filepath.Dir(path)

	// To avoid names being defined twice on the profiles, the name
	// is only defined when referring to a profile which results
	// on the .name field of Profiles not being populated.
	for k, v := range cfg.Profiles {
		v.Name = k
		cfg.Profiles[k] = v
	}

	// Profiles are the allowlist every workload is checked against, and
	// their fields reach file paths and command lines. Validate them at
	// load time rather than at the point of use.
	for k, v := range cfg.Profiles {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("invalid profile %q: %w", k, err)
		}
	}

	if cfg.Gateway != nil {
		if err := cfg.Gateway.Validate(cfg.RootDir); err != nil {
			return nil, fmt.Errorf("invalid gateway: %w", err)
		}
	}

	return cfg, nil
}
