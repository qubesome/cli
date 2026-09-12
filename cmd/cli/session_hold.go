package cli

import (
	"context"
	"log/slog"

	"github.com/qubesome/cli/internal/gateway"
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
			err := session.Current().Hold()

			// The gateway goes with the session, and it is taken down
			// here because nothing else can. A gateway is started inside
			// this namespace and outlives the launch that started it, so
			// once this process goes the gateway that is left is one
			// whose namespaces are owned by a user namespace nothing can
			// reach any more: the next launch would find it alive, wire
			// nothing to it, and fail at the veth with a message that
			// names nothing.
			//
			// gateway.startOnce catches that case too, and has to: a
			// holder that is killed outright never reaches this line.
			// This is what keeps the ordinary shutdown from leaving one
			// behind at all.
			//
			// It runs whether or not holding failed, and its own failure
			// is a warning rather than the answer. A session that is
			// ending has nothing better to do about a gateway it could
			// not stop than say so, and the failure of the hold is the
			// more useful of the two to report.
			if pid, stopErr := gateway.Current().Stop(); stopErr != nil {
				slog.Warn("failed to stop the session gateway as the session ended; "+
					"the next launch will replace it", "error", stopErr)
			} else if pid > 0 {
				slog.Debug("[session] stopped the session gateway as the session ended", "pid", pid)
			}

			return err
		},
	}
	return cmd
}
