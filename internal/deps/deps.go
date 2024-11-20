package deps

import (
	"fmt"
	"os"
	"os/exec"

	"text/tabwriter"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
)

var (
	red   = "\033[31m"
	green = "\033[32m"
	amber = "\033[33m"
	reset = "\033[0m"
)

var deps map[string][]string = map[string][]string{
	"clip": {
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
	"start": {
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
	"start": {
		files.FireCrackerBinary,
		files.DbusBinary,
	},
}

func Run(_ ...command.Option[interface{}]) error {
	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 5, ' ', 0)
	fmt.Fprintln(writer, "Command\tDependency\tStatus")
	fmt.Fprintln(writer, "-------\t----------\t------")

	for name, d := range deps {
		for _, dn := range d {
			_, err := exec.LookPath(dn)
			status := green + "OK" + reset
			if err != nil {
				status = red + "NOT FOUND" + reset
			}

			fmt.Fprintf(writer, "%s\t%s\t%s\n", name, dn, status)
		}

		if opt, ok := optionalDeps[name]; ok {
			for _, dn := range opt {
				_, err := exec.LookPath(dn)
				status := green + "OK" + reset
				if err != nil {
					status = amber + "NOT FOUND (Optional)" + reset
				}

				fmt.Fprintf(writer, "%s\t%s\t%s\n", name, dn, status)
			}
		}
	}

	writer.Flush()

	return nil
}
