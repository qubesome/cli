package cmd

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
)

var deps map[string][]string = map[string][]string{
	"clipboard": {
		files.XclipBinary,
		files.ShBinary,
	},
	"run": {
		files.DockerBinary,
	},
	"xdg-open": {
		files.DockerBinary,
	},
	"images": {
		files.DockerBinary,
	},
	"profiles": {
		files.DockerBinary,
		files.XauthBinary,
		files.MCookieBinary,
		files.SedBinary,
		files.ShBinary,
	},
}

var optionalDeps map[string][]string = map[string][]string{
	"run": {
		files.FireCrackerBinary,
		files.CloudHypervisorBinary,
	},
	"xdg-open": {
		files.FireCrackerBinary,
		files.CloudHypervisorBinary,
	},
	"images": {
		files.FireCrackerBinary,
		files.CloudHypervisorBinary,
	},
	"profiles": {
		files.FireCrackerBinary,
		files.CloudHypervisorBinary,
	},
}

func depsCmd(args []string, _ *types.Config) error {
	f := flag.NewFlagSet("", flag.ExitOnError)
	err := f.Parse(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	slog.Debug("cmd", "args", args)

	if len(f.Args()) != 1 || f.Arg(0) != "show" {
		depsUsage()
	}

	for name, d := range deps {
		fmt.Printf("%s: ", name)

		if len(d) == 0 {
			fmt.Println("OK")
			continue
		} else {
			fmt.Println()
		}

		for _, dn := range d {
			_, err := exec.LookPath(dn)
			status := "OK"
			if err != nil {
				status = "NOT FOUND"
			}

			fmt.Printf("- %s: %s\n", dn, status)
		}

		if opt, ok := optionalDeps[name]; ok {
			for _, dn := range opt {
				_, err := exec.LookPath(dn)
				status := "OK"
				if err != nil {
					status = "NOT FOUND (Optional)"
				}

				fmt.Printf("- %s: %s\n", dn, status)
			}
		}

		fmt.Println()
	}

	return nil
}

func depsUsage() {
	fmt.Printf(
		"usage: %[1]s deps show\n", execName)
	os.Exit(1)
}
