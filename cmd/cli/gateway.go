package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/gateway"
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
// Not profileConfigOrDefault. That falls back to the user-level config as
// soon as more than one profile is active, and the gateway is session
// wide: it was started by whichever launch found none running, from that
// profile's config, and may have come from any of them. Reporting the
// user-level file's gateway block for it would name an image, a policy and
// a subnet that the running gateway need not have anything to do with.
//
// Several active profiles are usually not an ambiguity at all, because one
// qubesome config commonly defines several profiles and they were all
// started from the same file. It is only profiles started from different
// files that leave nothing here able to say which one the gateway came
// from, and then saying so is the answer.
func sessionConfig() (*types.Config, string) {
	active := activeConfigs()

	resolved := make([]string, 0, len(active))
	for _, path := range active {
		// The run dir holds a symlink per active profile, so two profiles
		// sharing a config are two links to one file.
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		resolved = append(resolved, target)
	}

	if path, ok := sessionConfigPath(resolved); ok {
		if cfg := config(path); cfg != nil && len(cfg.Profiles) > 0 {
			return cfg, ""
		}
	}

	if len(resolved) > 1 {
		return nil, fmt.Sprintf(
			"%d profiles are active and were started from different configs, so which of them the "+
				"running gateway came from cannot be told, and the image, policy and subnet it "+
				"names are unknown", len(resolved))
	}

	// No profile running, or one whose config no longer reads. The
	// user-level file is the only thing left that describes a gateway.
	return profileConfigOrDefault(""), ""
}

// sessionConfigPath returns the one config file every active profile was
// started from, and whether there was one.
func sessionConfigPath(active []string) (string, bool) {
	if len(active) == 0 {
		return "", false
	}

	for _, path := range active[1:] {
		if path != active[0] {
			return "", false
		}
	}

	return active[0], true
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
