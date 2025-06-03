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
				Description: `Examples:

qubesome clip from-host                        - Copy clipboard contents from host to the active profile
qubesome clip from-host -type image/png        - Copy image from host clipboard to the active profile
qubesome clip from-host -profile <name>        - Copy clipboard contents from host to a specific profile
`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "target_profile",
						UsageText:   "Required when multiple profiles are active",
						Destination: &targetProfile,
					},
				},
				Flags: []cli.Flag{
					clipType,
				},
				ShellComplete: func(ctx context.Context, cmd *cli.Command) {
					if cmd.NArg() > 0 {
						return
					}
					for _, p := range activeProfiles() {
						fmt.Println(p)
					}
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
						Destination: &sourceProfile,
					},
					&cli.StringArg{
						Name:        "target_profile",
						Destination: &targetProfile,
					},
				},
				Flags: []cli.Flag{
					clipType,
				},
				ShellComplete: func(ctx context.Context, cmd *cli.Command) {
					if cmd.NArg() > 1 {
						return
					}
					for _, p := range activeProfiles() {
						fmt.Println(p)
					}
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
				Description: `Examples:

qubesome clip to-host                        - Copy clipboard contents from the active profile to the host
qubesome clip to-host -type image/png        - Copy image from the active profile clipboard to the host
qubesome clip to-host -profile <name>        - Copy clipboard contents from a specific profile to the host
				`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "source_profile",
						UsageText:   "Required when multiple profiles are active",
						Destination: &sourceProfile,
					},
				},
				Flags: []cli.Flag{
					clipType,
				},
				ShellComplete: func(ctx context.Context, cmd *cli.Command) {
					if cmd.NArg() > 0 {
						return
					}
					for _, p := range activeProfiles() {
						fmt.Println(p)
					}
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
