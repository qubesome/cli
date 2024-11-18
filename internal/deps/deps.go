package deps

import (
	"fmt"
	"os/exec"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
)

var deps map[string][]string = map[string][]string{
	"clipboard": {
		files.XclipBinary,
		files.ShBinary,
	},
	"run": {
		files.ContainerRunnerBinary,
	},
	"xdg-open": {
		files.ContainerRunnerBinary,
	},
	"images": {
		files.ContainerRunnerBinary,
	},
	"profiles": {
		files.ContainerRunnerBinary,
		files.ShBinary,
		files.XrandrBinary,
	},
}

var optionalDeps map[string][]string = map[string][]string{
	"run": {
		files.FireCrackerBinary,
		files.DbusBinary,
	},
	"xdg-open": {
		files.FireCrackerBinary,
	},
	"images": {
		files.FireCrackerBinary,
	},
	"profiles": {
		files.FireCrackerBinary,
		files.DbusBinary,
	},
}

func Run(_ ...command.Option[interface{}]) error {
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
