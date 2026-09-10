package doctor

import (
	"fmt"
	"strings"

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
	// No config and no gateway block are different answers. A bare
	// qubesome doctor loads a config only through a running profile, so
	// without one it knows nothing about a gateway rather than knowing
	// there is none. Reporting the second for the first is how this said
	// "no gateway is configured" on a host whose config configures one.
	if cfg == nil {
		return []Check{checkNoConfig()}
	}

	if cfg.Gateway == nil {
		return []Check{checkNoGateway()}
	}

	holder := checkHolder(env)
	gateway := checkGateway(env, holder.Status == OK)

	checks := []Check{holder, gateway}
	if gateway.Status == OK {
		// Readiness and egress only say something once there is a
		// gateway to ask about. Piling a second failure on top of the
		// same cause would bury the one worth reading.
		checks = append(checks, checkGatewayReady(env), checkGatewayEgress(env))
	}

	return checks
}

// checkNoConfig reports that nothing could be read about the session.
//
// It is a warning and not a failure. The session may be perfectly well,
// and this says only that the question was not answerable, which is a
// different thing from an answer.
func checkNoConfig() Check {
	return Check{
		Name:   "session gateway",
		Status: Warn,
		Detail: "no qubesome config was loaded, so whether a gateway is configured is unknown",
		Fix: "Name a profile, as in `qubesome doctor <profile>`, or run this from a directory " +
			"whose config qubesome can find.",
	}
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
		// Warn and not Fail. Both the holder and the gateway are started
		// by the first launch that needs one, so a host that has simply
		// not opened such a workload yet is idle rather than broken. The
		// profile sandbox check reads the same way for the same reason.
		return Check{
			Name:   name,
			Status: Warn,
			Detail: fmt.Sprintf("no holder is recorded as running in %s", path),
			Fix: "A workload on a gateway network is started inside the session's user namespace, " +
				"and the holder is what keeps that namespace open. It is started by the first " +
				"such launch, so this is expected until one runs.",
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
// held says whether the session's user namespace holder is running, which
// is what tells an idle session from a broken one. Both come up together
// on the first launch that needs them, so neither running is a session
// nobody has asked for. A holder without a gateway is the broken case:
// something got far enough to open the namespace and then did not finish.
func checkGateway(env Env, held bool) Check {
	const name = "session gateway"

	path := files.GatewayStatePath()

	if !env.SandboxAlive(path) {
		if !held {
			return Check{
				Name:   name,
				Status: Warn,
				Detail: fmt.Sprintf("a gateway is configured and none is running, recorded in %s", path),
				Fix: "It comes up with the first workload on a gateway network, together with the " +
					"session holder, and neither is running. This is expected until one is launched.",
			}
		}

		return Check{
			Name:   name,
			Status: Fail,
			Detail: fmt.Sprintf("the session is open but no gateway is recorded as running in %s", path),
			Fix: "The session holder is up, so a launch got as far as opening the namespace and " +
				"then did not bring the gateway up. Every workload on a gateway network fails " +
				"closed until it is there. Start one and read the error.",
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
			Fix: "Its resolver, proxy or netfilter ruleset did not come up. Read `qubesome gateway " +
				"logs` for what it said, and check the policy file the gateway block names.",
		}
	}

	return Check{
		Name:   name,
		Status: OK,
		Detail: "the gateway reports its resolver, proxy and ruleset are up",
	}
}

// checkGatewayEgress reports what the gateway has been doing with the
// connections its workloads made.
//
// A refused connection and a failed one are not the same finding. A
// denial is the policy doing what it says, so it is reported without
// being called a fault, and the hosts are named because "why can I not
// reach this" is what brings someone here. An error is something the
// gateway tried to do and could not, which is worth a warning.
func checkGatewayEgress(env Env) Check {
	const name = "gateway egress"

	summary, err := env.GatewayLog()
	if err != nil {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: fmt.Sprintf("the gateway is running but what it has said cannot be read: %s",
				firstLine(err.Error())),
			Fix: "A gateway started before qubesome kept a log has none. Restart the session to " +
				"get one, or read the terminal the gateway was started from.",
		}
	}

	if summary.Errors > 0 {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: fmt.Sprintf("the gateway reported %s, most recently %q",
				plural(summary.Errors, "error"), summary.LastError),
			Fix: "Read `qubesome gateway logs` for the whole of it. `-workload` and `-profile` " +
				"narrow it to one workload.",
		}
	}

	if summary.Denied == 0 {
		return Check{
			Name:   name,
			Status: OK,
			Detail: fmt.Sprintf("the gateway classified %s and refused none",
				plural(summary.Decisions, "connection")),
		}
	}

	return Check{
		Name:   name,
		Status: OK,
		Detail: fmt.Sprintf("the gateway classified %s and refused %d, to %s",
			plural(summary.Decisions, "connection"),
			summary.Denied, strings.Join(summary.DeniedHosts, ", ")),
		Fix: "This is the policy being applied, and is only a problem if one of those hosts was " +
			"meant to be reachable. A workload reaches a host named under egress.allowed or " +
			"dns.allowed for it, and the gateway mirrors one onto the other when only one is set.",
	}
}

// plural renders a count with its noun, so a report reads as a sentence
// rather than as a field.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}
