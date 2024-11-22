package cli

import (
	"context"

	"github.com/qubesome/cli/internal/qubesome"
	"github.com/urfave/cli/v3"
)

func xdgCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "xdg-open",
		Aliases: []string{"xdg"},
		Usage:   "opens a file or URL in the user's configured workload",
		Description: `Examples:

qubesome xdg-open https://github.com/qubesome                       - Opens the URL on the workload defined on the active qubesome config
qubesome xdg-open -profile <profile> https://github.com/qubesome    - Opens the URL on the workload defined on the given profile's qubesome config
`,
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

			return qubesome.XdgRun(
				qubesome.WithConfig(cfg),
				qubesome.WithExtraArgs(cmd.Args().Slice()),
				qubesome.WithRunner(runner),
			)
		},
	}
	return cmd
}
