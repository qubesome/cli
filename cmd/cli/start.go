package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/profiles"
	"github.com/urfave/cli/v3"
)

var detach bool

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
				Usage:       "sets what runner to be used, this will override the value set at the qubesome.config. Options: docker or podman.",
			},
			&cli.BoolFlag{
				Name:        "interactive",
				Aliases:     []string{"i"},
				Destination: &interactive,
				Usage:       "enables interactive mode, which runs the profile container but holds any windows manager execution. Use this for troubleshooting.",
			},
			&cli.BoolFlag{
				Name:        "detach",
				Aliases:     []string{"d"},
				Destination: &detach,
				Usage:       "start the profile process in the background. This cannot be used in conjunction with --interactive nor --debug.",
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
			// When a profile is started, it starts an inception server
			// so that the containerised Windows Manager is able to execute
			// new container workloads.
			// Running on detached mode makes a background call to qubesome,
			// leaving it running so that the main process can exit right away.
			if !debug && !interactive && detach {
				var args []string
				for _, arg := range os.Args[1:] {
					if arg == "-d" || arg == "-detach" {
						continue
					}
					args = append(args, arg)
				}
				cmd := exec.Command(os.Args[0], args...) //nolint
				cmd.Env = append(cmd.Env, os.Environ()...)
				cmd.Stdout = nil
				cmd.Stderr = nil

				cmd.SysProcAttr = &syscall.SysProcAttr{
					Setsid: true,
				}

				if err := cmd.Start(); err != nil {
					return fmt.Errorf("failed to run profile start in detach mode: %w", err)
				}

				fmt.Printf("[%d] %q profile start detached\n", cmd.Process.Pid, targetProfile)
				os.Exit(0)
			}

			opts := []command.Option[profiles.Options]{
				profiles.WithProfile(targetProfile),
				profiles.WithGitURL(gitURL),
				profiles.WithPath(path),
				profiles.WithLocal(local),
				profiles.WithRunner(runner),
			}

			if interactive {
				opts = append(opts, profiles.WithInteractive())
			}

			return profiles.Run(opts...)
		},
	}
	return cmd
}
