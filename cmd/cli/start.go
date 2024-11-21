package cli

import (
	"context"

	"github.com/qubesome/cli/internal/profiles"
	"github.com/urfave/cli/v3"
)

func startCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "start",
		Aliases: []string{"s"},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "git",
				Usage:       "git repository URL",
				Destination: &gitURL,
			},
			&cli.StringFlag{
				Name:        "path",
				Usage:       "rel path (based on -git / -path) to the dir containing the qubesome.config",
				Destination: &path,
			},
			&cli.StringFlag{
				Name:        "local",
				Usage:       "local is the local path for a git repository. This is to be used in combination with --git.",
				Destination: &local,
			},
			&cli.StringFlag{
				Name:        "runner",
				Destination: &runner,
			},
		},
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "profile",
				Min:         1,
				Max:         1,
				Destination: &targetProfile,
			},
		},
		Usage: "start qubesome profiles",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfg := profileConfigOrDefault(targetProfile)

			return profiles.Run(
				profiles.WithConfig(cfg),
				profiles.WithProfile(targetProfile),
				profiles.WithGitURL(gitURL),
				profiles.WithPath(path),
				profiles.WithLocal(local),
				profiles.WithRunner(runner),
			)
		},
	}
	return cmd
}
