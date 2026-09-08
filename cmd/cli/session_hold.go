package cli

import (
	"context"

	"github.com/qubesome/cli/internal/session"
	"github.com/urfave/cli/v3"
)

// sessionHoldCommand is the command of the sandbox that holds the
// session's user namespace open. qubesome starts it as
// bwrap --unshare-user --dev-bind / / -- qubesome session-hold, and every
// other sandbox of the session nests its own user namespace inside that
// one.
//
// It is hidden, as supervise and profile-display are, because it is
// qubesome calling itself. Run by hand it would hold a namespace nothing
// knows how to reach, and take the session lock away from the holder that
// should have it.
//
// Being a subcommand rather than a variable read from the environment
// follows what supervise established: this entrypoint is written by
// qubesome, so it can say plainly what it is.
func sessionHoldCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   session.HoldCommand,
		Hidden: true,
		Usage:  "Holds a user namespace open for the session",
		Description: `Not intended to be called directly. qubesome uses it as the
command of the sandbox that owns the session's user namespace:

qubesome session-hold
`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return session.Current().Hold()
		},
	}
	return cmd
}
