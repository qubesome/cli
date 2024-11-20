package cli

import (
	"context"
	"fmt"

	"github.com/qubesome/cli/internal/clipboard"
	"github.com/qubesome/cli/internal/command"
	"github.com/urfave/cli/v3"
)

func clipboardCommand() *cli.Command {
	clipType := &cli.StringFlag{
		Name:    "type",
		Aliases: []string{"t"},
		Validator: func(s string) error {
			switch s {
			case "image/png":
				return nil
			}
			return fmt.Errorf("unsupported type %q", s)
		},
	}

	cmd := &cli.Command{
		Name:    "clipboard",
		Aliases: []string{"clip"},
		Usage:   "enable sharing of clipboard across profiles and the host",
		Commands: []*cli.Command{
			{
				Name: "from-host",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "target_profile",
						Min:         1,
						Max:         1,
						Destination: &targetProfile,
					},
				},
				Flags: []cli.Flag{
					clipType,
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					cfg := profileConfigOrDefault(targetProfile)

					target, ok := cfg.Profiles[targetProfile]
					if !ok {
						return fmt.Errorf("no active profile %q found", targetProfile)
					}

					opts := []command.Option[clipboard.Options]{
						clipboard.WithFromHost(),
						clipboard.WithTargetProfile(target),
					}

					if typ := c.String("type"); typ != "" {
						fmt.Println(typ)
						opts = append(opts, clipboard.WithContentType(typ))
					}

					return clipboard.Run(
						opts...,
					)
				},
			},
			{
				Name: "from-profile",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "source_profile",
						Min:         1,
						Max:         1,
						Destination: &sourceProfile,
					},
					&cli.StringArg{
						Name:        "target_profile",
						Min:         1,
						Max:         1,
						Destination: &targetProfile,
					},
				},
				Flags: []cli.Flag{
					clipType,
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					cfg := profileConfigOrDefault(targetProfile)

					source, ok := cfg.Profiles[sourceProfile]
					if !ok {
						return fmt.Errorf("no active profile %q found", sourceProfile)
					}

					target, ok := cfg.Profiles[targetProfile]
					if !ok {
						return fmt.Errorf("no active profile %q found", targetProfile)
					}

					opts := []command.Option[clipboard.Options]{
						clipboard.WithSourceProfile(source),
						clipboard.WithTargetProfile(target),
					}

					if typ := c.String("type"); typ != "" {
						fmt.Println(typ)
						opts = append(opts, clipboard.WithContentType(typ))
					}

					return clipboard.Run(
						opts...,
					)
				},
			},
		},
	}
	return cmd
}
