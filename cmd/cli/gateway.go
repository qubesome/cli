package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/gateway"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/session"
	"github.com/qubesome/cli/internal/types"
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
var (
	logsFollow   bool
	logsLast     int
	logsWorkload string
)

func gatewayCommand() *cli.Command {
	cmd := &cli.Command{
		Name:   "gateway",
		Hidden: true,
		Usage:  "inspects and stops the session gateway",
		Description: `qubesome starts, reuses and reloads the session gateway on its own,
so this is for the cases where that is not enough:

qubesome gateway status  - Report what qubesome knows about the session's gateway
qubesome gateway stop    - Stop the session's gateway, leaving the session itself up
qubesome gateway logs    - Show what the session's gateway has said

A running gateway is reused whatever image it came from, so a change to
the gateway block of the config reaches nothing until it is stopped. The
next launch then starts a fresh one inside the same session.
`,
		Commands: []*cli.Command{
			gatewayStatusCommand(),
			gatewayStopCommand(),
			gatewayLogsCommand(),
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

			cfg, problem := sessionConfig()

			// Inspect reports that it could not tell rather than leaving
			// the lines blank, so a config that could not be identified
			// is passed on as none with the reason it could not.
			status := g.Inspect(session.Current(), cfg, g.StatusReady)
			if problem != "" {
				status.ConfigProblem = problem
			}

			return status.Write(os.Stdout)
		},
	}
}

// sessionConfig returns the config describing this session's gateway, and
// why it could not be told when it cannot.
//
// It is read from the record the gateway's own launch wrote rather than
// inferred from whichever profiles happen to be active. Inference answered
// wrongly exactly when it mattered: a gateway is session wide and outlives
// the profile that started it, so once that profile has stopped there is
// nothing left among the active ones to point at, and the nearest config
// is a guess that names an image, a policy and a subnet the running
// gateway need have nothing to do with.
//
// Not profileConfigOrDefault, for the same reason. Its fallbacks are right
// for a launch, which is choosing a config to act on, and wrong here,
// where the question is which config something already running came from.
//
// With no record and no gateway running, the user-level file is the
// answer: there is nothing whose provenance could be got wrong, and what a
// status then describes is the gateway this host would start.
func sessionConfig() (*types.Config, string) {
	g := gateway.Current()

	if path, ok := g.RecordedConfig(); ok {
		if cfg := config(path); cfg != nil {
			return cfg, ""
		}

		return nil, fmt.Sprintf(
			"the running gateway was started from %s, which no longer reads as a config, "+
				"so the image, policy and subnet it names are unknown", path)
	}

	// A gateway with no record is one started before qubesome kept one, or
	// one whose record could not be written. Either way nothing here can
	// say where it came from, and a nearby config would be a guess.
	if sandbox.Alive(files.GatewayStatePath()) {
		return nil, "the running gateway has no record of the config it was started from, " +
			"so the image, policy and subnet it names are unknown"
	}

	return profileConfigOrDefault(""), ""
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

// gatewayLogsCommand shows what the gateway has said.
//
// The gateway is started by whichever qubesome run found none running, and
// it is put in a session of its own so that a Ctrl-C at that terminal does
// not take the session's egress with it. Its output has nowhere to go that
// anybody is still watching, so it is written to a file, and this is how it
// is read back. It is the record of which host a workload was allowed or
// refused, which is the one thing needed when a workload cannot reach
// something it should.
func gatewayLogsCommand() *cli.Command {
	return &cli.Command{
		Name:  "logs",
		Usage: "show the session gateway's logs",
		Description: `Examples:

qubesome gateway logs                          - Print the log of the gateway this session is running
qubesome gateway logs -n 50                    - Print its last 50 lines
qubesome gateway logs -f                       - Print it and keep printing what is added
qubesome gateway logs -profile work            - Only the lines about that profile's workloads
qubesome gateway logs -workload chrome         - Only the lines about that workload, in any profile
qubesome gateway logs -profile work -workload chrome
                                               - Only the lines about that one workload

The log covers the gateway that is running. Starting a gateway begins it
afresh, so there is nothing here for a session that has not started one.

The gateway knows a workload as its name and its profile's joined by a
dash, and either half may hold a dash of its own, so naming only one of
the two matches the other loosely. Naming both is exact. A line about no
workload, such as the gateway's own startup, is not shown when either
filter is given.
`,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "follow",
				Aliases:     []string{"f"},
				Usage:       "keep printing what is added to the log",
				Destination: &logsFollow,
			},
			&cli.StringFlag{
				Name:        "profile",
				Usage:       "only the lines about the workloads of this profile",
				Destination: &targetProfile,
			},
			&cli.StringFlag{
				Name:        "workload",
				Usage:       "only the lines about this workload",
				Destination: &logsWorkload,
			},
			&cli.IntFlag{
				Name:        "lines",
				Aliases:     []string{"n"},
				Usage:       "print only this many of the log's last matching lines",
				Destination: &logsLast,
			},
		},
		Action: func(ctx context.Context, _ *cli.Command) error {
			return gateway.ShowLogs(ctx, os.Stdout, gateway.LogOptions{
				Path:     files.GatewayLogPath(),
				Profile:  targetProfile,
				Workload: logsWorkload,
				Last:     logsLast,
				Follow:   logsFollow,
			})
		},
	}
}
