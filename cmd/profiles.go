package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/qubesome/cli/internal/profiles"
	"github.com/qubesome/cli/internal/types"
)

func profilesCmd(args []string, cfg *types.Config) error {
	slog.Debug("cmd", "args", args)
	if len(args) < 1 || args[0] != "run" {
		profilesUsage()
	}

	var name string
	f := flag.NewFlagSet("", flag.ExitOnError)
	f.StringVar(&name, "name", "", "")
	err := f.Parse(args[1:])
	if err != nil {
		return err
	}

	if cfg == nil {
		return fmt.Errorf(`err: could not load config`)
	}

	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	profile.Name = name

	return profiles.Start(profile, cfg)
}

func profilesUsage() {
	fmt.Printf(`usage: %s profiles run <NAME>`, execName)
	os.Exit(1)
}
