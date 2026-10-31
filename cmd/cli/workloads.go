package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/gateway"
	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/workloads"
	"github.com/urfave/cli/v3"
)

func workloadsCommand() *cli.Command {
	return &cli.Command{
		Name:    "workloads",
		Aliases: []string{"ps"},
		Usage:   "list the workloads that are running, and kill or restart them",
		Description: `Examples:

qubesome workloads                          - List every workload, in every profile
qubesome workloads kill chrome              - Kill the chrome workload
qubesome workloads kill -profile work chrome - Kill it in the work profile, when more than one runs it
qubesome workloads kill -all                - Kill every workload, in every profile
qubesome workloads restart chrome           - Kill the chrome workload and run it again

The list is read from the records each launch leaves behind, so it also
shows a record whose workload has gone. Such a record is reported as stale,
and is what stops a single instance workload from starting again: killing
the workload removes it.
`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return listWorkloads(os.Stdout)
		},
		Commands: []*cli.Command{
			{
				Name:    "list",
				Aliases: []string{"ls"},
				Usage:   "list the workloads that are running",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return listWorkloads(os.Stdout)
				},
			},
			workloadsKillCommand(),
			workloadsRestartCommand(),
		},
	}
}

func listWorkloads(w io.Writer) error {
	records, err := workloads.List(files.RunUserQubesome())
	if err != nil {
		return err
	}

	if len(records) == 0 {
		fmt.Fprintln(w, "no workloads are running")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tWORKLOAD\tPID\tADDRESS\tSTATUS")
	for _, r := range records {
		status := "running"
		if !r.Running {
			status = "stale"
		}

		addr := r.Address
		if addr == "" {
			addr = "-"
		}

		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", r.Profile, r.Workload, r.PID, addr, status)
	}

	return tw.Flush()
}

func workloadsKillCommand() *cli.Command {
	var (
		name    string
		profile string
		all     bool
	)

	return &cli.Command{
		Name:  "kill",
		Usage: "kill a workload, or every workload with -all",
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "workload",
				Destination: &name,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        profileFlag,
				Usage:       "the profile the workload runs in, required when more than one runs it",
				Destination: &profile,
			},
			&cli.BoolFlag{
				Name:        "all",
				Usage:       "kill every workload, in every profile",
				Destination: &all,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := refuseInside(); err != nil {
				return err
			}

			records, err := workloads.List(files.RunUserQubesome())
			if err != nil {
				return err
			}

			if all {
				if name != "" {
					return errors.New("-all kills every workload, so it takes no workload name")
				}
				if profile != "" {
					records = workloads.Find(records, "", profile)
				}

				var errs []error
				for _, r := range records {
					errs = append(errs, killWorkload(ctx, os.Stdout, r))
				}
				return errors.Join(errs...)
			}

			if name == "" {
				return errors.New("name the workload to kill, or pass -all")
			}

			r, err := workloads.One(records, name, profile)
			if err != nil {
				return err
			}

			return killWorkload(ctx, os.Stdout, r)
		},
	}
}

func workloadsRestartCommand() *cli.Command {
	var (
		name    string
		profile string
	)

	return &cli.Command{
		Name:  "restart",
		Usage: "kill a workload and run it again",
		Description: `The workload is run again the way qubesome run would run it, so
arguments given to the launch that started it are not carried over.
A workload that is not running is simply run.
`,
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "workload",
				Destination: &name,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        profileFlag,
				Usage:       "the profile the workload runs in, required when more than one runs it",
				Destination: &profile,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if err := refuseInside(); err != nil {
				return err
			}

			if name == "" {
				return errors.New("name the workload to restart")
			}

			records, err := workloads.List(files.RunUserQubesome())
			if err != nil {
				return err
			}

			r, err := workloads.One(records, name, profile)
			switch {
			case errors.Is(err, workloads.ErrNotRecorded):
			case err != nil:
				return err
			default:
				if err := killWorkload(ctx, os.Stdout, r); err != nil {
					return err
				}
				profile = r.Profile
			}

			return launchWorkload(name, profile, "", false, nil)
		},
	}
}

// refuseInside stops a command that reads the host's run directory from
// being run inside a profile, where that directory is not there to read.
func refuseInside() error {
	if inception.Inside() {
		return errors.New("workloads can only be managed from the host, not from inside a profile")
	}
	return nil
}

// killWorkload kills one workload and tells the gateway it has gone.
//
// The gateway is told because the launch that started the workload is
// what would otherwise do it, as the workload exits, and a launch typed at
// a terminal has long since returned. A gateway that still maps the name
// refuses the next launch of it.
//
// Only the gateway the workload was wired to is told. One wired to a
// gateway that has gone is mapped by nothing running, and the name may by
// now be another workload's. See gateway.Gateway.Unregister.
func killWorkload(ctx context.Context, w io.Writer, r workloads.Record) error {
	if err := workloads.Kill(r); err != nil {
		return fmt.Errorf("failed to kill %s in profile %s: %w", r.Workload, r.Profile, err)
	}

	if r.Address != "" {
		gateway.Current().Unregister(ctx, r.Wiring(), r.Name)
	}

	if r.Running {
		fmt.Fprintf(w, "killed %s in profile %s, pid %d\n", r.Workload, r.Profile, r.PID)
	} else {
		fmt.Fprintf(w, "removed the stale record of %s in profile %s\n", r.Workload, r.Profile)
	}

	return nil
}
