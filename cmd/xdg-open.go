package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
)

func xdgOpenCmd(args []string, cfg *types.Config) error {
	socketAddress := files.InProfileSocketPath()
	slog.Debug("check whether running inside container")
	if _, err := os.Stat(socketAddress); err == nil {
		slog.Debug("dialing host qubesome", "socket", socketAddress)
		c, err := net.Dial("unix", socketAddress)
		if err != nil {
			return err
		}

		args = append([]string{"xdg-open"}, args...)
		slog.Debug("writing to socket", "args", args)

		_, err = c.Write([]byte(strings.Join(args, " ")))
		if err != nil {
			return err
		}
		return nil
	}

	var profile string
	fs := flag.NewFlagSet("", flag.ExitOnError)
	fs.StringVar(&profile, "profile", "untrusted", "The profile name which will be used to run the workload.")

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	slog.Debug("cmd", "args", args)

	if len(fs.Args()) != 1 {
		xdgOpenUsage()
	}

	q := qubesome.New()

	var in *qubesome.WorkloadInfo

	// If user level config was not found, try to load the config
	// for the target profile which at this point must be started.
	if cfg == nil {
		if profile == "" {
			return fmt.Errorf("profile is required when no global config is present")
		}

		cfgPath := files.ProfileConfig(profile)
		slog.Debug("trying to load config from started profile", "path", cfgPath)

		target, err := os.Readlink(cfgPath)
		if err != nil {
			return fmt.Errorf("cannot read profile symlink: %w", err)
		}
		cfgPath = target

		c, err := types.LoadConfig(cfgPath)
		if err != nil {
			return fmt.Errorf("could load config (check profile is loaded): %w", err)
		}
		cfg = c

		p, ok := cfg.Profiles[profile]
		if !ok {
			return fmt.Errorf("failed to find profile %s", in.Profile)
		}

		pp, err := securejoin.SecureJoin(filepath.Dir(cfgPath), p.Path)
		if err != nil {
			return err
		}
		slog.Debug("override path", "path", pp)
		in = &qubesome.WorkloadInfo{
			Profile: profile,
			Path:    pp,
		}
	}

	q.Config = cfg

	return q.HandleMime(in, fs.Args())
}

func xdgOpenUsage() {
	fmt.Printf(`usage: %s xdg-open https://google.com
`, execName)
	os.Exit(1)
}
