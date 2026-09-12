package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/profiles"
	"github.com/urfave/cli/v3"
)

var detach bool

// detachedVar tells the child of a detached start that it is the child.
//
// A detached start runs this same command line again in the background,
// so the child has to be stopped from detaching a second time. That used
// to be done by filtering the flag out of os.Args, and the filter knew
// two of its spellings: "-d" and "-detach". urfave/cli accepts "--detach"
// as well, which went straight through, so the child detached again, and
// its child did, and so on without end. Measured against urfave/cli
// v3.11.0: all three spellings set the flag.
//
// An environment variable has no spellings, so there is nothing left to
// miss, and the argument list is passed on exactly as it was typed rather
// than being rebuilt by string surgery.
const detachedVar = "QUBESOME_DETACHED"

// detachNow decides what a start does with --detach.
//
// isChild says this process is the background half of a detached start,
// which must go on and do the work rather than detaching again. Its own
// argument list still carries --detach, because the list is passed on
// exactly as it was typed, so this is the only thing that stops it.
//
// The conflict is refused rather than ignored. Detaching discards the
// child's output, so there is nowhere for a debug log to go and no
// terminal to hold an interactive shell open on, and a start that quietly
// stayed in the foreground after being asked for the background was a
// flag that did nothing and said nothing about it.
func detachNow(detach, debug, interactive, isChild bool) (bool, error) {
	if !detach {
		return false, nil
	}

	if debug || interactive {
		return false, errors.New("--detach cannot be used with --debug or --interactive: " +
			"a detached start has no terminal to write a log to or to hold a shell open on")
	}

	return !isChild, nil
}

// locatable reports whether a start has been told where its config is.
//
// With no -git, -local or -path, a start reads qubesome.config out of the
// working directory, so running one from anywhere else failed with a bare
// "no such file or directory" naming a relative path. The config qubesome
// last opened is almost always the one meant, and naming it here turns
// that into a command to copy.
//
// It names it rather than using it. Where a start is told its config from
// decides what ${GITDIR} expands to in the profile, and a fallback that
// quietly picked a different one of those would change what every mapped
// path in the profile resolves to.
func locatable(gitURL, local, path string) error {
	if gitURL != "" || local != "" || path != "" {
		return nil
	}

	if _, err := os.Stat(profiles.ConfigName); err == nil {
		return nil
	}

	remembered, ok := files.RememberedConfig()
	if !ok {
		return fmt.Errorf("no %s in the working directory: say where the config is with -git, -local or -path",
			profiles.ConfigName)
	}

	return fmt.Errorf("no %s in the working directory. The last config qubesome opened is %s, "+
		"so start from it with: qubesome start -path %s %s",
		profiles.ConfigName, remembered, filepath.Dir(remembered), targetProfile)
}

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
				Destination: &targetProfile,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			background, err := detachNow(detach, debug, interactive, os.Getenv(detachedVar) != "")
			if err != nil {
				return err
			}

			// When a profile is started, it starts an inception server
			// so that the containerised Windows Manager is able to execute
			// new container workloads.
			// Running on detached mode makes a background call to qubesome,
			// leaving it running so that the main process can exit right away.
			if background {
				// The whole command line, unedited. See detachedVar.
				child := exec.Command(os.Args[0], os.Args[1:]...) //nolint
				child.Env = append(os.Environ(), detachedVar+"=1")
				child.Stdout = nil
				child.Stderr = nil

				child.SysProcAttr = &syscall.SysProcAttr{
					Setsid: true,
				}

				if err := child.Start(); err != nil {
					return fmt.Errorf("failed to run profile start in detach mode: %w", err)
				}

				fmt.Printf("[%d] %q profile start detached\n", child.Process.Pid, targetProfile)
				os.Exit(0)
			}

			if err := locatable(gitURL, local, path); err != nil {
				return err
			}

			opts := []command.Option[profiles.Options]{
				profiles.WithProfile(targetProfile),
				profiles.WithGitURL(gitURL),
				profiles.WithPath(path),
				profiles.WithLocal(local),
			}

			if interactive {
				opts = append(opts, profiles.WithInteractive())
			}

			return profiles.Run(opts...)
		},
	}
	return cmd
}
