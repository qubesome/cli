package cli

import (
	"context"

	"github.com/qubesome/cli/internal/deps"
	"github.com/urfave/cli/v3"
)

func depsCommand() *cli.Command {
	cmd := &cli.Command{
		Name:  "deps",
		Usage: "shows status of external dependencies",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "runner",
				Destination: &runner,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg := profileConfigOrDefault("")
			return deps.Run(deps.WithConfig(cfg),
				deps.WithRunner(runner),
			)
		},
	}
	return cmd
}
