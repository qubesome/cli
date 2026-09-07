package doctor

import (
	"fmt"

	"github.com/qubesome/cli/internal/types"
)

// Options are what the doctor command was asked to examine.
type Options struct {
	Config   *types.Config
	Runner   string
	Profile  string
	Workload string
	Colour   bool
}

// Run builds the report. It returns the report rather than printing it,
// so that what is checked and how it is shown stay separable.
func Run(env Env, o Options) *Report {
	report := &Report{}

	// The environment section reports on the container runner, so it has
	// to be told which one. A profile names its own, and a host with both
	// installed otherwise gets asked about the runner it is not using.
	runner := o.Runner
	if profile, ok := o.Config.Profile(o.Profile); ok {
		runner = runnerFor(runner, *profile)
	}

	report.Add("Environment", Environment(env, runner))

	if o.Profile == "" {
		return report
	}

	report.Add(fmt.Sprintf("profile %s", o.Profile), Profile(env, o.Config, o.Runner, o.Profile))

	if o.Workload == "" {
		return report
	}

	report.Add(fmt.Sprintf("workload %s/%s", o.Profile, o.Workload),
		Workload(env, o.Config, o.Runner, o.Profile, o.Workload))

	return report
}
