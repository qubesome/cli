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
		Usage: "execute workloads",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg := profileConfigOrDefault(targetProfile)

			return qubesome.Run(
				qubesome.WithWorkload(workload),
				qubesome.WithProfile(targetProfile),
				qubesome.WithConfig(cfg),
				qubesome.WithRunner(runner),
				qubesome.WithExtraArgs(cmd.Args().Slice()),
			)
		},
	}
	return cmd
}
