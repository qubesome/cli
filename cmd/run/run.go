package run

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"flag"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
)

func Command(args []string, cfg *types.Config) error {
	socketAddress := files.InProfileSocketPath()
	slog.Debug("check whether running inside container")
	if _, err := os.Stat(socketAddress); err == nil {
		fmt.Println("dialing host qubesome", "socket", socketAddress)
		c, err := net.Dial("unix", socketAddress)
		if err != nil {
			return err
		}
		args = append([]string{"run"}, args...)
		_, err = c.Write([]byte(strings.Join(args, " ")))
		if err != nil {
			return err
		}
		return nil
	}

	in := qubesome.WorkloadInfo{}

	fs := flag.NewFlagSet("", flag.ExitOnError)
	fs.StringVar(&in.Profile, "profile", "untrusted", "The profile name which will be used to run the workload.")

	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	if in.Profile == "" || fs.NArg() != 1 {
		runUsage(os.Args[0])
	}

	in.Name = fs.Arg(0)

	// If user level config was not found, try to load the config
	// for the target profile which at this point must be started.
	if cfg == nil {
		cfgPath := files.ProfileConfig(in.Profile)
		slog.Debug("trying to load config from started profile", "path", cfgPath)

		target, err := os.Readlink(cfgPath)
		if err != nil {
			return fmt.Errorf("cannot read profile symlink: %w", err)
		}
		cfgPath = target

		cfg, err = types.LoadConfig(cfgPath)
		if err != nil {
			return fmt.Errorf("could load config (check profile is loaded): %w", err)
		}

		profile, ok := cfg.Profiles[in.Profile]
		if !ok {
			return fmt.Errorf("failed to find profile %s", in.Profile)
		}

		pp, err := securejoin.SecureJoin(filepath.Dir(cfgPath), profile.Path)
		if err != nil {
			return err
		}
		slog.Debug("override path", "path", pp)
		in.Path = pp
	}

	q := qubesome.New()
	q.Config = cfg

	wg := sync.WaitGroup{}
	if err := cfg.WorkloadPullMode.Pull(&wg); err != nil {
		return err
	}
	// Wait for any background operation that is in-flight.
	defer wg.Wait()

	if fs.NArg() > 0 {
		in.Args = args[len(args)-fs.NArg():]
		slog.Debug("extra args", "args", in.Args)
	}

	return q.Run(in)
}

func runUsage(name string) {
	fmt.Printf(`usage: %s run -profile untrusted chrome
`, name)
	os.Exit(1)
}
