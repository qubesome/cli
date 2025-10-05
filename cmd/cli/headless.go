package cli

import (
	"context"

	"github.com/qubesome/cli/internal/qubesome"
	"github.com/urfave/cli/v3"
)

var conf string

func headlessCommand() *cli.Command {
	cmd := &cli.Command{
		Name:  "headless",
		Usage: "execute workloads in headless mode",
		Description: `Examples:

qubesome headless -profile <path> <workload>
qubesome headless -profile ~/git/dotfiles chrome
`,
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "workload",
				Destination: &workload,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "config",
				Destination: &conf,
			},
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
			cfg := config(conf)

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
