package cli

import (
	"context"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/urfave/cli/v3"
)

// superviseCommand is the entrypoint of a single instance workload's
// sandbox. It runs the workload's command and spawns siblings into the
// namespaces that command is already in, which is the only way to reach
// them: a sandbox cannot be joined from outside.
//
// It is hidden, as profile-display and usb are, because it is qubesome
// calling itself. Nothing outside a sandbox has anywhere to serve, since
// the socket it listens on only exists where it is bound in.
//
// Being a subcommand rather than a variable read from the environment is
// the difference from inception.Inside, which infers where it is running
// because it has no command line of its own to look at. This entrypoint is
// written by qubesome, so it can say plainly what it is.
func superviseCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   sandbox.SuperviseCommand,
		Hidden: true,
		Usage:  "Runs a workload inside its sandbox and spawns siblings into it on request",
		Description: `Not intended to be called directly. qubesome uses it as the
entrypoint of a single instance workload's sandbox, and of any workload
that is given an address on the session gateway:

qubesome supervise [--gated] <command> [args...]

With --gated the workload is not started until the host has wired the
sandbox to the gateway and said so on the supervisor's socket.
`,
		// Everything after the command name belongs to the workload, and
		// most workloads pass flags of their own. Parsing them here would
		// consume them or fail on them, so --gated is read by hand from
		// the position qubesome writes it in.
		SkipFlagParsing: true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			argv := cmd.Args().Slice()

			gated := len(argv) > 0 && argv[0] == sandbox.GatedFlag
			if gated {
				argv = argv[1:]
			}

			return sandbox.Supervise(files.InWorkloadAgentSocket(), argv, gated)
		},
	}
	return cmd
}
