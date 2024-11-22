package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/qubesome/cli/internal/images"
	"github.com/urfave/cli/v3"
)

func imagesCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "images",
		Aliases: []string{"i"},
		Usage:   "manage workload images",
		Commands: []*cli.Command{
			{
				Name: "pull",
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
					cfg := profileConfigOrDefault(targetProfile)
					if cfg == nil {
						return errors.New("could not find qubesome config")
					}

					if targetProfile != "" {
						if _, ok := cfg.Profile(targetProfile); !ok {
							return fmt.Errorf("could not find profile %q", targetProfile)
						}
					}

					return images.Run(
						images.WithConfig(cfg),
						images.WithRunner(runner),
					)
				},
			},
		},
	}
	return cmd
}
