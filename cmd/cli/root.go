package cli

import (
	"context"
	"errors"
	"fmt"
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
			versionCommand(),
			completionCommand(),
			hostRunCommand(),
		},
	}

	cmd.Before = func(ctx context.Context, c *cli.Command) (context.Context, error) {
		if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
			fmt.Println("\033[33mWARN: Running qubesome in Wayland is experimental. Some features may not work as expected.\033[0m")
		}
		return ctx, nil
	}

	cmd.Flags = append(cmd.Flags, &cli.BoolFlag{
		Name:        "debug",
		Value:       false,
		Destination: &debug,
		Sources:     cli.EnvVars("QS_DEBUG"),
		Action: func(ctx context.Context, c *cli.Command, b bool) error {
			if debug {
				return log.Configure("DEBUG", true, false, false)
			}
			return nil
		},
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
	cfg, err := types.LoadConfig(path)
	if err != nil {
		return nil
	}
	cfg.RootDir = filepath.Dir(path)

	return cfg
}

func profileConfigOrDefault(profile string) *types.Config {
	path := files.ProfileConfig(profile)
	target, err := os.Readlink(path)

	var c *types.Config
	if err == nil {
		c = config(target)
	}

	if c != nil {
		return c
	}

	path = files.QubesomeConfig()
	return config(path)
}

func profileOrActive(profile string) (*types.Profile, error) {
	if profile != "" {
		cfg := profileConfigOrDefault(profile)
		prof, ok := cfg.Profiles[profile]
		if !ok {
			return nil, fmt.Errorf("profile %q not active", profile)
		}
		return prof, nil
	}

	matches, err := filepath.Glob(filepath.Join(files.RunUserQubesome(), "*.config"))
	if err != nil {
		return nil, err
	}
	if len(matches) > 1 {
		return nil, errors.New("multiple profiles active: pick one with -profile")
	}
	if len(matches) == 0 {
		return nil, errors.New("no active profile found: start one with qubesome start")
	}

	f := matches[0]
	name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
	return profileOrActive(name)
}
