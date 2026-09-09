package cli

import (
	"context"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/urfave/cli/v3"
)

// vmInitCommand is the entrypoint of a microVM guest. The kernel command
// line reaches it through init=, so it runs as pid 1 of the machine: it
// makes the filesystems an OCI image does not carry, reaps everything the
// machine orphans, and supervises the workload the same way a bwrap
// sandbox is supervised.
//
// It is hidden for the reason supervise is hidden: it is qubesome calling
// itself, and nothing outside a guest has a machine to be the init of.
//
// It takes no command, which is the difference from supervise. What to
// run is composed into the image as /etc/qubesome/init.json, because the
// only other way into a guest is the kernel command line, which is
// bounded in length and world readable through /proc/cmdline.
func vmInitCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   sandbox.VMInitCommand,
		Hidden: true,
		Usage:  "Runs as the init of a qubesome microVM",
		Description: `Not intended to be called directly. qubesome uses it as the
init of a microVM guest, named by init= on the kernel command line:

qubesome vm-init
`,
		// Flag parsing is skipped as it is for supervise, and here for a
		// second reason: a kernel passes init anything on the command
		// line it did not recognise itself, and a boot must not fail
		// over one of those being read as a flag.
		SkipFlagParsing: true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return sandbox.VMInit()
		},
	}
	return cmd
}
