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
		Usage:   "start qubesome profiles",
		Description: `Examples:

qubesome start -git https://github.com/qubesome/sample-dotfiles awesome
qubesome start -git https://github.com/qubesome/sample-dotfiles i3
`,
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
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return profiles.Run(
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
