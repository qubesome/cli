package cli

import (
	"context"

	"github.com/qubesome/cli/internal/qubesome"
	"github.com/urfave/cli/v3"
)

func runCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "run",
		Aliases: []string{"r"},
		Usage:   "execute workloads",
		Description: `Examples:

qubesome run chrome                        - Run the chrome workload on the active profile
qubesome run -profile <profile> chrome     - Run the chrome workload on a specific profile
`,
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "workload",
				Min:         1,
				Max:         1,
				Destination: &workload,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "profile",
				Destination: &targetProfile,
			},
			&cli.StringFlag{
				Name:        "runner",
				Destination: &runner,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			prof, err := profileOrActive(targetProfile)
			if err != nil {
				return err
			}

			cfg := profileConfigOrDefault(prof.Name)

			return qubesome.Run(
				qubesome.WithWorkload(workload),
				qubesome.WithProfile(prof.Name),
				qubesome.WithConfig(cfg),
				qubesome.WithRunner(runner),
				qubesome.WithExtraArgs(cmd.Args().Slice()),
			)
		},
	}
	return cmd
}
