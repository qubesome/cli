package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/qubesome/cli/internal/gateway"
	"github.com/qubesome/cli/internal/session"
	"github.com/urfave/cli/v3"
)

// gatewayCommand reports on the session's gateway and takes it down.
//
// It is hidden, but not for the reason supervise and session-hold are.
// Nothing here runs inside a sandbox. It is hidden because qubesome manages
// the gateway itself: a launch starts one, reuses the one it finds and asks
// it to re-read its policy, so the ordinary way to get a gateway is to run a
// workload and there is nothing for a user to do here. What is left is an
// operator's business, and the one thing a launch will not do is replace a
// running gateway with one from a different image.
//
// Neither subcommand takes a profile. There is one gateway per session and
// not one per profile, because the policy it applies is keyed by workload
// across every profile.
func gatewayCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   "gateway",
		Hidden: true,
		Usage:  "inspects and stops the session gateway",
		Description: `qubesome starts, reuses and reloads the session gateway on its own,
so this is for the cases where that is not enough:

qubesome gateway status  - Report what qubesome knows about the session's gateway
qubesome gateway stop    - Stop the session's gateway, leaving the session itself up

A running gateway is reused whatever image it came from, so a change to
the gateway block of the config reaches nothing until it is stopped. The
next launch then starts a fresh one inside the same session.
`,
		Commands: []*cli.Command{
			gatewayStatusCommand(),
			gatewayStopCommand(),
		},
	}
	return cmd
}

func gatewayStatusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "reports what qubesome knows about the session gateway",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			g := gateway.Current()

			// The same route doctor takes to a config, and it may find
			// none. The gateway is per session, so there is no profile to
			// name here, and without a running profile or a user-level
			// file there is nothing that says which image, policy or
			// subnet a gateway was meant to have. Inspect reports that it
			// could not tell rather than leaving the lines blank.
			status := g.Inspect(session.Current(), profileConfigOrDefault(""), g.StatusReady)

			return status.Write(os.Stdout)
		},
	}
}

func gatewayStopCommand() *cli.Command {
	return &cli.Command{
		Name:  "stop",
		Usage: "stops the session gateway, leaving the session itself up",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			pid, err := gateway.Current().Stop()
			if err != nil {
				return err
			}

			if pid == 0 {
				fmt.Fprintln(os.Stdout, "no gateway is running for this session")
				return nil
			}

			fmt.Fprintf(os.Stdout,
				"stopped the session gateway, pid %d. The session is still up, "+
					"so the next launch starts a fresh gateway inside it.\n", pid)

			return nil
		},
	}
}
