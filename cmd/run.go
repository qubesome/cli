package cmd

import (
	"fmt"
	"os"
	"sync"

	"flag"

	"github.com/qubesome/qubesome-cli/internal/qubesome"
	"github.com/qubesome/qubesome-cli/internal/types"
)

func runCmd(args []string, cfg *types.Config) error {
	in := qubesome.WorkloadInfo{}

	fs := flag.NewFlagSet("", flag.ExitOnError)
	fs.StringVar(&in.Name, "name", "", fmt.Sprintf("The name of the workload to be executed. For new workloads use %s import first.", execName))
	fs.StringVar(&in.Profile, "profile", "untrusted", "The profile name which will be used to run the workload.")
	fs.Parse(args)

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

	extraArgs := flag.Args()
	if len(extraArgs) > 1 {
		in.Args = extraArgs[1:]
	}

	return q.Run(in)
}

func runUsage() {
	fmt.Printf(`usage: %s run -name chrome -profile untrusted
`, execName)
	os.Exit(1)
}
