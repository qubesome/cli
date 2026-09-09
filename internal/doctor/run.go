package doctor

import (
	"fmt"

	"github.com/qubesome/cli/internal/types"
)

// Options are what the doctor command was asked to examine.
type Options struct {
	Config   *types.Config
	Profile  string
	Workload string
	Colour   bool
}

// Run builds the report. It returns the report rather than printing it,
// so that what is checked and how it is shown stay separable.
func Run(env Env, o Options) *Report {
	report := &Report{}

	report.Add("Environment", Environment(env))

	// The session sits between the host and a profile. It is one per user
	// rather than one per profile, so it is reported whether or not a
	// profile was named.
	report.Add("Session", Session(env, o.Config))

	if o.Profile == "" {
		return report
	}

	report.Add(fmt.Sprintf("profile %s", o.Profile), Profile(env, o.Config, o.Profile))

	if o.Workload == "" {
		return report
	}

	report.Add(fmt.Sprintf("workload %s/%s", o.Profile, o.Workload),
		Workload(env, o.Config, o.Profile, o.Workload))

	return report
}
