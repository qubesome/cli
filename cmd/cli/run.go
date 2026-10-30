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
				Name:        profileFlag,
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
			return launchWorkload(workload, targetProfile, runner, limited, cmd.Args().Slice())
		},
	}
	return cmd
}

// launchWorkload starts a workload the way qubesome run does. It is apart
// from the command so that a restart launches exactly what a run would.
func launchWorkload(workload, profile, runner string, limited bool, args []string) error {
	var cfg *types.Config

	// Commands that can be executed from within a profile
	// (a.k.a. inception mode) should not check for profile
	// names nor configs, as those are imposed by the inception
	// server.
	if !inception.Inside() {
		prof, err := profileOrActive(profile)
		if err != nil {
			return err
		}

		profile = prof.Name
		cfg = profileConfigOrDefault(profile)

		if runner == "" {
			runner = prof.Runner
		}
	}

	opts := []command.Option[qubesome.Options]{
		qubesome.WithWorkload(workload),
		qubesome.WithProfile(profile),
		qubesome.WithConfig(cfg),
		qubesome.WithRunner(runner),
		qubesome.WithExtraArgs(args),
	}

	if limited {
		opts = append(opts, qubesome.WithLimited())
	}

	return qubesome.Run(opts...)
}
