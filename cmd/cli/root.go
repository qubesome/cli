package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/log"
	"github.com/qubesome/cli/internal/types"
	"github.com/urfave/cli/v3"
)

var (
	targetProfile string
	sourceProfile string
	gitURL        string
	workload      string
	path          string
	local         string
	runner        string
	commandName   string
	debug         bool
	interactive   bool
)

func RootCommand() *cli.Command {
	cmd := &cli.Command{
		Commands: []*cli.Command{
			startCommand(),
			runCommand(),
			imagesCommand(),
			clipboardCommand(),
			xdgCommand(),
			depsCommand(),
			doctorCommand(),
			versionCommand(),
			completionCommand(),
			hostRunCommand(),
			profileDisplayCommand(),
			flatpakCommand(),
			headlessCommand(),
			usbCommand(),
			gpuCommand(),
			superviseCommand(),
			sessionHoldCommand(),
			gatewayCommand(),
			tunnelCommand(),
			vmInitCommand(),
			consoleCommand(),
		},
	}

	// This runs ahead of every command, and several of them have a caller
	// reading their standard output as data rather than as text for a
	// person: tunnel puts an ssh connection through it, completion is
	// eval'd by a shell, and clipboard writes back what was pasted. A
	// warning on stdout is read as the far end talking, as shell input, or
	// as part of the paste, so it goes to stderr for the same reason
	// main.go sends a returned error there. It is still seen: stderr is
	// where ssh shows what a ProxyCommand says.
	cmd.Before = func(ctx context.Context, c *cli.Command) (context.Context, error) {
		// The level is decided here rather than in the flag's own Action
		// so that there is one place that sets it and no question about
		// which of the two runs last.
		//
		// A run says what it did at INFO whether or not --debug was
		// asked for: which session it is in, which gateway it used, what
		// address it was given, what it wired. Those are the facts a
		// launch that goes wrong is diagnosed from, and leaving them
		// behind --debug meant the first report of any failure never had
		// them.
		level := "INFO"
		if debug {
			level = "DEBUG"
		}

		if err := log.Configure(level, true, false, false); err != nil {
			return ctx, err
		}

		if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
			fmt.Fprintln(os.Stderr,
				"\033[33mWARN: Running qubesome in Wayland is experimental. Some features may not work as expected.\033[0m")
		}
		return ctx, nil
	}

	cmd.Flags = append(cmd.Flags, &cli.BoolFlag{
		Name: "debug",
		// Every other flag says what it is for, and this one printed a
		// bare name in the global options of every help screen.
		Usage:       "log at DEBUG rather than INFO",
		Value:       false,
		Destination: &debug,
		Sources:     cli.EnvVars("QS_DEBUG"),
	})
	cmd.Version = shortVersion()
	cmd.Usage = "A cli to GitOps your dotfiles"
	cmd.Suggest = true
	cmd.EnableShellCompletion = true

	return cmd
}

func config(path string) *types.Config {
	if _, err := os.Stat(path); err != nil {
		return nil
	}

	// A started profile's config is reached through a symlink in the run
	// dir. The root dir has to be the directory the config was sourced
	// from, since a profile's path and every path mapped into it descend
	// from that, and the run dir holds none of them.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	cfg, err := types.LoadConfig(path)
	if err != nil {
		return nil
	}
	cfg.Source = path
	cfg.RootDir = filepath.Dir(path)

	return cfg
}

func profileConfigOrDefault(profile string) *types.Config {
	if profile != "" {
		// Try to load the profile specific config.
		path := files.ProfileConfig(profile)
		target, err := os.Readlink(path)
		slog.Debug("try to load profile config", "profile", profile, "path", path, target, "target")

		if err == nil {
			c := config(target)
			slog.Debug("using profile config", "path", path, "config", c)
			if c != nil {
				return c
			}
		}
	}

	cfgs := activeConfigs()
	if len(cfgs) == 1 {
		c := config(cfgs[0])
		slog.Debug("using active profile config", "path", cfgs[0], "config", c)
		if c != nil && len(c.Profiles) > 0 {
			return c
		}
	}

	// Try to load user-level qubesome config.
	path = files.QubesomeConfig()
	c := config(path)
	slog.Debug("using user-level config", "path", path, "config", c)
	if c != nil && len(c.Profiles) > 0 {
		return c
	}

	return nil
}

func profileOrActive(profile string) (*types.Profile, error) {
	if profile != "" {
		cfg := profileConfigOrDefault(profile)
		prof, ok := cfg.Profile(profile)
		if !ok {
			return nil, fmt.Errorf("profile %q not active", profile)
		}
		return prof, nil
	}

	cfgs := activeConfigs()
	if len(cfgs) > 1 {
		return nil, errors.New("multiple profiles active: pick one with -profile")
	}
	if len(cfgs) == 0 {
		return nil, errors.New("no active profile found: start one with qubesome start")
	}

	f := cfgs[0]
	name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
	return profileOrActive(name)
}

func activeConfigs() []string {
	var active []string

	root := files.RunUserQubesome()
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			fn := entry.Name()
			if filepath.Ext(fn) == ".config" {
				active = append(active, filepath.Join(root, fn))
			}
		}
	}

	return active
}

func activeProfiles() []string {
	cfgs := activeConfigs()
	profiles := make([]string, 0, len(cfgs))

	for _, file := range cfgs {
		profiles = append(profiles, strings.TrimSuffix(filepath.Base(file), ".config"))
	}

	return profiles
}
