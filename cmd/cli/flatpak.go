package cli

import (
	"context"

	"github.com/qubesome/cli/internal/flatpak"
	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/types"
	"github.com/urfave/cli/v3"
)

func flatpakCommand() *cli.Command {
	cmd := &cli.Command{
		Name: "flatpak",
		Commands: []*cli.Command{
			{
				Name:  "run",
				Usage: "execute Flatpak workloads from the host into a Qubesome profile",
				Description: `Examples:

qubesome flatpak run org.kde.francis                        - Run the org.kde.francis flatpak on the active profile
qubesome flatpak run -profile <profile> org.kde.francis     - Run the org.kde.francis flatpak on a specific profile
`,
				Arguments: []cli.Argument{
					&cli.StringArg{
						Name:        "workload",
						Min:         1,
						Max:         1,
						Destination: &workload,
					},
				},
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "profile",
						Destination: &targetProfile,
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					var cfg *types.Config

					// Commands that can be executed from within a profile
					// (a.k.a. inception mode) should not check for profile
					// names nor configs, as those are imposed by the inception
					// server.
					if !inception.Inside() {
						prof, err := profileOrActive(targetProfile)
						if err != nil {
							return err
						}

						targetProfile = prof.Name
						cfg = profileConfigOrDefault(targetProfile)
					}

					return flatpak.Run(
						flatpak.WithName(workload),
						flatpak.WithProfile(targetProfile),
						flatpak.WithConfig(cfg),
						flatpak.WithExtraArgs(cmd.Args().Slice()),
					)
				},
			},
		},
	}
	return cmd
}
