package doctor

import (
	"fmt"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
)

// Session diagnoses the pieces one qubesome session shares between every
// sandbox it starts: the process holding its user namespace open, and the
// gateway that gives its workloads egress.
//
// cfg may be nil, which reads the same as a config with no gateway block.
// A user whose config did not load is told so by the profile section, and
// repeating it here would say nothing new.
func Session(env Env, cfg *types.Config) []Check {
	if cfg == nil || cfg.Gateway == nil {
		return []Check{checkNoGateway()}
	}

	holder, gateway := checkHolder(env), checkGateway(env)

	checks := []Check{holder, gateway}
	if gateway.Status == OK {
		// Readiness only says something once there is a gateway to ask.
		// Piling a second failure on top of the same cause would bury
		// the one worth reading.
		checks = append(checks, checkGatewayReady(env))
	}

	return checks
}

// checkNoGateway reports a configuration with no gateway block.
//
// It is an intentional absence and not a finding. No gateway means no
// egress for any workload, which is exactly what a sandbox without one
// does, and that has to stay a configuration a user can choose. Saying so
// is still worth a line, because a user who expected egress and does not
// have it is otherwise left with a report that mentions nothing about it.
func checkNoGateway() Check {
	return Check{
		Name:   "session gateway",
		Status: OK,
		Detail: "no gateway is configured",
		Fix: "Workloads run with no egress, which is a supported configuration. Add a gateway " +
			"block to the qubesome config to give them one.",
	}
}

// checkHolder reports on the process holding the session's user namespace
// open.
//
// Every sandbox that needs a gateway address is started inside that
// namespace, so without a holder there is nothing for one to nest in and
// no veth can be created for it.
func checkHolder(env Env) Check {
	const name = "session holder"

	path := files.SessionStatePath()

	if !env.SandboxAlive(path) {
		return Check{
			Name:   name,
			Status: Fail,
			Detail: fmt.Sprintf("no holder is recorded as running in %s", path),
			Fix: "A workload on a gateway network is started inside the session's user namespace, " +
				"and the holder is what keeps that namespace open. It is started by the first " +
				"such launch, so run one and read the error if it does not come up.",
		}
	}

	return Check{
		Name:   name,
		Status: OK,
		Detail: fmt.Sprintf("the holder recorded in %s is running", path),
	}
}

// checkGateway reports whether the session's gateway sandbox is running.
//
// sandbox.Alive on the state file and not a look at the socket. A state
// file outlives the process it names, and Alive records the start time
// alongside the pid so one left behind by a crash reads as not running.
func checkGateway(env Env) Check {
	const name = "session gateway"

	path := files.GatewayStatePath()

	if !env.SandboxAlive(path) {
		return Check{
			Name:   name,
			Status: Fail,
			Detail: fmt.Sprintf("a gateway is configured but none is recorded as running in %s", path),
			Fix: "Every workload on a gateway network needs it, and a launch fails closed rather " +
				"than giving one egress with no policy on it. The gateway comes up with the first " +
				"such launch, so start one and read the error if it does not.",
		}
	}

	return Check{
		Name:   name,
		Status: OK,
		Detail: fmt.Sprintf("the gateway recorded in %s is running", path),
	}
}

// checkGatewayReady asks the gateway itself whether it is ready.
//
// Through the control client and not by looking for the socket file. A
// socket on disk says only that a listener bound, which happens before the
// resolver, the proxy and the netfilter ruleset are up, and a workload
// started in that window would have egress with no rules on it. Readiness
// is the answer to that call and nothing else.
func checkGatewayReady(env Env) Check {
	const name = "gateway readiness"

	if err := env.GatewayReady(); err != nil {
		return Check{
			Name:   name,
			Status: Fail,
			Detail: fmt.Sprintf("the gateway is running but did not report itself ready: %s", firstLine(err.Error())),
			Fix: "Its resolver, proxy or netfilter ruleset did not come up. Check the gateway's own " +
				"output, and the policy file the gateway block names.",
		}
	}

	return Check{
		Name:   name,
		Status: OK,
		Detail: "the gateway reports its resolver, proxy and ruleset are up",
	}
}
