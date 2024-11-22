package cli

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/urfave/cli/v3"
)

func hostRunCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "host-run",
		Aliases: []string{"hr"},
		Usage:   "Runs a command at the host, but shows it in a given qubesome profile",
		Description: `Examples:

qubesome host-run firefox                        - Run firefox on the host and display it on the active profile
qubesome host-run -profile <profile> firefox     - Run firefox on the host and display it on a specific profile
`,
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "command",
				Min:         1,
				Max:         1,
				Destination: &commandName,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "profile",
				Usage:       "Required when multiple profiles are active",
				Destination: &targetProfile,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			prof, err := profileOrActive(targetProfile)
			if err != nil {
				return err
			}

			c := exec.Command(commandName)
			c.Env = append(c.Env, fmt.Sprintf("DISPLAY=:%d", prof.Display))
			out, err := c.CombinedOutput()
			fmt.Println(out)

			return err
		},
	}
	return cmd
}
