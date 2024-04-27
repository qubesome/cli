package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/profiles"
	"github.com/qubesome/cli/internal/types"
)

const startUsagef = `usage:
    %[1]s start <profile>
    %[1]s start -git=https://github.com/qubesome/dotfiles-example -path / <profile>
`

func startCmd(args []string, cfg *types.Config) error {
	slog.Debug("cmd", "args", args)
	if len(args) < 1 {
		startUsage()
	}

	var gitURL, path string
	f := flag.NewFlagSet("", flag.ExitOnError)
	f.StringVar(&gitURL, "git", "", "Defines a git repository source")
	f.StringVar(&path, "path", "", "Dir path that contains qubesome.config")

	err := f.Parse(args)
	if err != nil {
		return err
	}

	if f.NArg() != 1 {
		startUsage()
	}

	name := f.Arg(0)
	if gitURL != "" {
		return profiles.StartFromGit(name, gitURL, path)
	}

	if cfg == nil {
		return fmt.Errorf(`err: could not load config`)
	}

	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found", name)
	}

	ep, err := securejoin.SecureJoin(files.QubesomeDir(), profile.Path)
	if err != nil {
		return err
	}
	profile.Path = ep

	return profiles.Start(profile, cfg)
}

func startUsage() {
	fmt.Printf(startUsagef, execName)
	os.Exit(1)
}
