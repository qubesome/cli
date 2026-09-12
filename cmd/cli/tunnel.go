package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/qubesome/cli/internal/gateway"
	"github.com/urfave/cli/v3"
)

// tunnelCommand reaches a host through the session gateway's proxy.
//
// It is hidden for the reason supervise and profile-display are: it runs
// inside a sandbox and is qubesome calling itself. What calls it is the
// ProxyCommand qubesome writes into the sandbox's ssh config, and there is
// nothing outside a sandbox for it to do, because the endpoint it needs is
// only in a workload's environment.
//
// It is here rather than left to socat because the sandbox already has the
// qubesome binary, having been given it to be wired to the gateway at all,
// and does not have socat. Doing it here also keeps the CONNECT exchange
// in something that can be tested, rather than in a config string nothing
// reads until an ssh fails.
func tunnelCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   "tunnel",
		Hidden: true,
		Usage:  "carries a connection to a host through the session gateway",
		Description: `Not intended to be called directly. qubesome points a workload's
ssh at it, as

    ProxyCommand qubesome tunnel %h %p

The gateway drops every port but 80, 443 and 53, so a workload reaches
anything else by asking the gateway's proxy to carry it. The endpoint to
ask is in QUBESOME_GATEWAY_PROXY, which a launch puts in the environment
of every workload it attaches to a gateway.

The connection is carried on standard input and output, so this speaks
whatever the client and the host speak and understands none of it. A host
the gateway's policy does not allow is refused by the gateway, and the
refusal is reported here with what it said.
`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) != 2 {
				return fmt.Errorf("usage: qubesome tunnel <host> <port>")
			}

			proxy, err := gateway.ProxyFromEnv()
			if err != nil {
				return err
			}

			return gateway.Tunnel(ctx, proxy, args[0], args[1], os.Stdin, os.Stdout)
		},
	}
	return cmd
}
