package cli

import (
	"context"

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
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					cfg := profileConfigOrDefault(targetProfile)

					return images.Run(images.WithConfig(cfg))
				},
			},
		},
	}
	return cmd
}
