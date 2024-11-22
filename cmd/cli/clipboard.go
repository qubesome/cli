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
				Name:  "from-host",
				Usage: "copies the clipboard contents from the host to a profile",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "target_profile",
						UsageText:   "Required when multiple profiles are active",
						Min:         0,
						Max:         1,
						Destination: &targetProfile,
					},
				},
				Flags: []cli.Flag{
					clipType,
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					target, err := profileOrActive(targetProfile)
					if err != nil {
						return err
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
				Name:  "from-profile",
				Usage: "copies the clipboard contents between profiles",
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

					source, ok := cfg.Profile(sourceProfile)
					if !ok {
						return fmt.Errorf("no active profile %q found", sourceProfile)
					}

					target, ok := cfg.Profile(targetProfile)
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
			{
				Name:  "to-host",
				Usage: "copies the clipboard contents from a profile to the host",
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "source_profile",
						UsageText:   "Required when multiple profiles are active",
						Min:         0,
						Max:         1,
						Destination: &sourceProfile,
					},
				},
				Flags: []cli.Flag{
					clipType,
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					target, err := profileOrActive(sourceProfile)
					if err != nil {
						return err
					}

					opts := []command.Option[clipboard.Options]{
						clipboard.WithSourceProfile(target),
						clipboard.WithTargetHost(),
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
