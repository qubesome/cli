package cli

import (
	"context"
	"os"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/urfave/cli/v3"
)

// defaultConsoleShell is what a console runs when it is given no command.
//
// A machine boots an arbitrary OCI image and nothing here knows what
// shells it carries. /bin/sh is the one a POSIX image is required to
// have, so it is the only defensible guess, and a workload that wants
// another names it.
const defaultConsoleShell = "/bin/sh"

// consoleCommand attaches the terminal it is run in to a microVM.
//
// It runs inside the sandbox of the workload that declared attachVM, not
// on the host. That is the whole shape of this: the terminal emulator is
// an ordinary bwrap workload with the machine's vsock socket bound into
// it, and the only thing that crosses into the guest is a pty. The
// alternative, a terminal inside the VM, would need an X server in the
// guest and a display socket reaching out of it.
//
// It is hidden for the reason supervise and vm-init are hidden: it is
// qubesome calling itself, and outside such a sandbox there is no socket
// to dial.
func consoleCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   sandbox.ConsoleCommand,
		Hidden: true,
		Usage:  "Attaches this terminal to the microVM the workload is bound to",
		Description: `Not intended to be called directly. qubesome uses it as the
command of a workload that declares attachVM:

qubesome console [command [args...]]
`,
		// Everything after the command name belongs to the guest, and a
		// shell run with flags is the ordinary case. Parsing them here
		// would consume them.
		SkipFlagParsing: true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			argv := cmd.Args().Slice()
			if len(argv) == 0 {
				argv = []string{defaultConsoleShell}
			}

			status, err := sandbox.ConsoleVM(files.InVMConsoleSocket(), sandbox.VMConsolePort, argv)
			if err != nil {
				return err
			}

			if status != 0 {
				// The status is the guest command's and the terminal's
				// user reads it as such. Returning an error would report
				// 1 for every one of them, since main maps every error
				// to that. The terminal has already been restored by the
				// time this runs.
				os.Exit(status)
			}

			return nil
		},
	}
	return cmd
}
