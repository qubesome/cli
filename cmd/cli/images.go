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
				// Named for what it does rather than how. It re-fetches
				// every image the config names whether or not the store
				// already holds one, which is a refresh and not a pull,
				// and a built image has nothing to pull at all.
				Name:    "refresh",
				Aliases: []string{"pull"},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "profile",
						Destination: &targetProfile,
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
					)
				},
			},
		},
	}
	return cmd
}
