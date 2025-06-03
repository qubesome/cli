package cli

import (
	"context"

	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
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
			var cfg *types.Config

			// Commands that can be executed from within a profile
			// (a.k.a. inception mode) should not check for profile
			// names nor configs, as those are imposed by the inception
			// server.
			if !inception.Inside() {
				prof, err := profileOrActive(targetProfile)
				if err != nil {
					return err
				}

				targetProfile = prof.Name
				cfg = profileConfigOrDefault(targetProfile)

				if runner == "" {
					runner = prof.Runner
				}
			}

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
