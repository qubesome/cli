package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/qubesome/cli/internal/doctor"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

func doctorCommand() *cli.Command {
	var target string

	cmd := &cli.Command{
		Name:  "doctor",
		Usage: "diagnoses a qubesome installation, profile or workload",
		Description: `Examples:

qubesome doctor                        - Check the host environment
qubesome doctor work                   - Also check the work profile
qubesome doctor work/chrome            - Also check the chrome workload of the work profile
`,
		Arguments: []cli.Argument{
			&cli.StringArg{
				Name:        "target",
				Destination: &target,
			},
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "runner",
				Destination: &runner,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			profile, workload := splitTarget(target)

			cfg := profileConfigOrDefault(profile)
			colour := term.IsTerminal(int(os.Stdout.Fd()))

			report := doctor.Run(doctor.NewOSEnv(), doctor.Options{
				Config:   cfg,
				Runner:   runner,
				Profile:  profile,
				Workload: workload,
				Colour:   colour,
			})

			if err := report.Write(os.Stdout, colour); err != nil {
				return err
			}

			if report.Failed() {
				return fmt.Errorf("doctor found %d failing check(s)", failedCount(report))
			}

			return nil
		},
	}
	return cmd
}

// failedCount counts the failing checks across a report, so the
// command's error can name how many failed rather than repeating detail
// already printed above it.
func failedCount(report *doctor.Report) int {
	n := 0
	for _, s := range report.Sections {
		for _, c := range s.Checks {
			if c.Status == doctor.Fail {
				n++
			}
		}
	}

	return n
}

// splitTarget splits a "profile" or "profile/workload" positional
// argument into its parts. An empty target yields two empty strings, so
// doctor checks only the environment.
func splitTarget(target string) (profile, workload string) {
	if target == "" {
		return "", ""
	}

	profile, workload, _ = strings.Cut(target, "/")

	return profile, workload
}
