package cli

import (
	"context"
	"fmt"

	"github.com/qubesome/cli/internal/qubesome"
	"github.com/urfave/cli/v3"
)

var conf string

func headlessCommand() *cli.Command {
	cmd := &cli.Command{
		Name:  "headless",
		Usage: "execute workloads in headless mode",
		Description: `Examples:

qubesome headless chrome                     - Run the chrome workload headless on the active profile
qubesome headless -profile work chrome       - Run it on a named profile
qubesome headless -config <path> chrome      - Run it against a config that no profile has started

-profile names a profile, the way it does everywhere else. -config is the
only flag here that takes a path, and it is for running a workload against
a config no profile has started; without it the active profile's config is
used, as qubesome run does.
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
				Usage:       "path to a qubesome config, for running against one no profile has started",
				Destination: &conf,
			},
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
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// A named config is used as it is given. Anything else here
			// would make a path that cannot be read fall back to whatever
			// profile happens to be running, which is a different config
			// than the one that was asked for.
			cfg := config(conf)

			if conf != "" && cfg == nil {
				return fmt.Errorf("the config %q could not be read", conf)
			}

			// With no config named, the active profile's is used, the way
			// qubesome run has always done it. Without this, headless was
			// the one command that could not be pointed at a running
			// profile, and saying nothing about which one it wanted it
			// reported "no config found".
			if cfg == nil {
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
				qubesome.WithHeadless(),
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
