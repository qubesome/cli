package cmd

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"

	"flag"

	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
)

const socketAddress = "/tmp/qube.sock"

func runCmd(args []string, cfg *types.Config) error {
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
	fs.StringVar(&in.Name, "name", "", fmt.Sprintf("The name of the workload to be executed. For new workloads use %s import first.", execName))
	fs.StringVar(&in.Profile, "profile", "untrusted", "The profile name which will be used to run the workload.")
	err := fs.Parse(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	if in.Name == "" || in.Profile == "" {
		runUsage()
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

func runUsage() {
	fmt.Printf(`usage: %s run -name chrome -profile untrusted
`, execName)
	os.Exit(1)
}
