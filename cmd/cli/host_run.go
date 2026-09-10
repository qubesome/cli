package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
// WAYLAND_DISPLAY is dropped because a toolkit that finds one connects to
// it and ignores DISPLAY, which would open the window on the host desktop
// rather than in the profile. The profile's own window manager is started
// without it for the same reason.
//
// The two entries are appended rather than substituted for the inherited
// ones. os/exec keeps the last value of a repeated key, so these are the
// values the command reads.
func hostRunEnv(base []string, display uint8, cookie string) []string {
	env := make([]string, 0, len(base)+2)
	for _, e := range base {
		if strings.HasPrefix(e, "WAYLAND_DISPLAY=") {
			continue
		}
		env = append(env, e)
	}

	return append(env,
		"DISPLAY=:"+strconv.Itoa(int(display)),
		"XAUTHORITY="+cookie,
	)
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
			out, err := c.CombinedOutput()
			fmt.Println(string(out))

			return err
		},
	}
	return cmd
}
