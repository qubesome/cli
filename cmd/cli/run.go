package cli

import (
	"context"

	"github.com/qubesome/cli/internal/command"
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
qubesome run -limited terminal             - Run the terminal workload with no gateway and no devices

Limited mode is the escape hatch. When the session gateway will not start
or a profile is half up, every launch fails at a stage that has nothing to
do with the workload, and a workload is what is needed to look at it. It
drops the gateway, every host device, the gpu, the bus and mime handling,
and never grants anything, so it can be asked for on any workload.
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
				Usage:       "the profile to run in, required when more than one is active",
				Destination: &targetProfile,
			},
			&cli.StringFlag{
				Name:        "runner",
				Usage:       "override the runner the workload or profile asks for",
				Destination: &runner,
			},
			&cli.BoolFlag{
				Name:        "limited",
				Usage:       "drop the gateway, host devices, the gpu, the bus and mime handling",
				Destination: &limited,
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

			opts := []command.Option[qubesome.Options]{
				qubesome.WithWorkload(workload),
				qubesome.WithProfile(targetProfile),
				qubesome.WithConfig(cfg),
				qubesome.WithRunner(runner),
				qubesome.WithExtraArgs(cmd.Args().Slice()),
			}

			if limited {
				opts = append(opts, qubesome.WithLimited())
			}

			return qubesome.Run(opts...)
		},
	}
	return cmd
}
