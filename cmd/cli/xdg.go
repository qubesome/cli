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
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "profile",
				Destination: &targetProfile,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg := profileConfigOrDefault(targetProfile)

			return qubesome.XdgRun(
				qubesome.WithConfig(cfg),
				qubesome.WithExtraArgs(cmd.Args().Slice()),
			)
		},
	}
	return cmd
}
