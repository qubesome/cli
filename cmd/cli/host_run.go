package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/urfave/cli/v3"
)

// hostRunEnv returns the environment for a command the host runs on a
// profile's display.
//
// The host environment is inherited rather than replaced. The command runs
// on the host as the user, with the user's own privileges, so withholding
// anything from it isolates nothing. Starting from an empty environment
// only takes away HOME and PATH, without which most host applications
// cannot find their own configuration, or a shell to run.
//
// DISPLAY and XAUTHORITY then point it at the profile instead of the host
// session. The cookie is the one the profile's workloads authenticate with,
// and the profile's X server refuses a connection that arrives without it,
// saying only that no authorization protocol was specified.
//
// What is dropped is the set of variables naming the session the command
// was launched from, which is not the session it is about to appear in.
// See launchingSession.
//
// The two entries are appended rather than substituted for the inherited
// ones. os/exec keeps the last value of a repeated key, so these are the
// values the command reads.
func hostRunEnv(base []string, display uint8, cookie string) []string {
	env := make([]string, 0, len(base)+2)
	for _, e := range base {
		if namesTheLaunchingSession(e) {
			continue
		}
		env = append(env, e)
	}

	return append(env,
		"DISPLAY=:"+strconv.Itoa(int(display)),
		"XAUTHORITY="+cookie,
	)
}

// launchingSession are the variables describing the display session the
// command was typed or bound in, rather than the profile it is being sent
// to. Each one is a handle on the host session, and the command is about
// to connect to a different display server, so each is either ignored
// there or acted on as if it meant something.
//
// WAYLAND_DISPLAY is a path to the host compositor. A toolkit that finds
// one connects to it and ignores DISPLAY, opening the window on the host
// desktop rather than in the profile. The profile's own window manager is
// started without it for the same reason.
//
// DESKTOP_STARTUP_ID is an X11 startup notification handed out by
// whatever launched qubesome. A window manager that spawns through
// startup notification records the workspace it spawned from against that
// id, and the application exports it to the next window it opens as
// _NET_STARTUP_ID. The profile's window manager then reads an id for a
// launch it never saw, and places the window by what that resolves to
// rather than on the workspace being looked at. It is also single use: it
// belongs to the launch that created it and to no later one.
//
// XDG_ACTIVATION_TOKEN is the Wayland spelling of the same thing, with
// the same two problems.
var launchingSession = []string{
	"WAYLAND_DISPLAY",
	"DESKTOP_STARTUP_ID",
	"XDG_ACTIVATION_TOKEN",
}

// namesTheLaunchingSession reports whether an environment entry is one of
// launchingSession.
func namesTheLaunchingSession(entry string) bool {
	name, _, ok := strings.Cut(entry, "=")
	if !ok {
		return false
	}

	return slices.Contains(launchingSession, name)
}

func hostRunCommand() *cli.Command {
	cmd := &cli.Command{
		Name:    "host-run",
		Aliases: []string{"hr"},
		Usage:   "Runs a command at the host, but shows it in a given qubesome profile",
		Description: `Examples:

qubesome host-run firefox                        - Run firefox on the host and display it on the active profile
qubesome host-run -profile <profile> firefox     - Run firefox on the host and display it on a specific profile
`,
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "command",
				Destination: &commandName,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "profile",
				Usage:       "Required when multiple profiles are active",
				Destination: &targetProfile,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			prof, err := profileOrActive(targetProfile)
			if err != nil {
				return err
			}

			cookie, err := files.ClientCookiePath(prof.Name)
			if err != nil {
				return err
			}

			c := exec.Command(commandName, cmd.Args().Slice()...) //nolint
			c.Env = hostRunEnv(os.Environ(), prof.Display, cookie)

			// This returns while the command keeps running, as launching
			// a workload does, so the terminal it was typed at is free
			// again. Its stdin is left closed rather than pointed at that
			// terminal, which the shell has taken back. Its output still
			// goes there, because a command that fails to reach the
			// profile's display says why on it.
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			if err := c.Start(); err != nil {
				return fmt.Errorf("failed to start %q: %w", commandName, err)
			}

			return nil
		},
	}
	return cmd
}
