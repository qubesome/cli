package cli

import (
	"context"
	"os"
	"path/filepath"

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
		},
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
