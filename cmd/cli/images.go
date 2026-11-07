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
						Name:        profileFlag,
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
			{
				// Takes no profile. It keeps the current and the previous
				// version of every image in the store, whichever config
				// names it, which is the same window refresh prunes to.
				Name:  "prune",
				Usage: "remove image versions older than the previous one",
				Action: func(context.Context, *cli.Command) error {
					return images.Prune()
				},
			},
		},
	}
	return cmd
}
