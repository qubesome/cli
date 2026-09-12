package doctor

import (
	"fmt"

	"github.com/qubesome/cli/internal/gateway"
	"github.com/qubesome/cli/internal/types"
)

// microVMChecks diagnose a running microVM's place on the session gateway.
//
// They read the live namespace rather than the records the launch wrote,
// because the failure worth catching here leaves exactly the records a
// working launch leaves. See wiringCheck.
func microVMChecks(env Env, cfg *types.GatewayConfig, sandboxPID int, addr string) []Check {
	w, err := env.MicroVMWiring(*cfg, sandboxPID)
	if err != nil {
		return []Check{{
			Name:   "microvm wiring",
			Status: Fail,
			Detail: fmt.Sprintf("could not read the microVM's network namespace: %s", err),
			Fix:    "Check that the session gateway is running, with qubesome gateway status.",
		}}
	}

	return []Check{
		{
			// Unconditionally OK, because the caller only reaches here for
			// a sandbox already read as running. It is in the report
			// anyway: the pid and the address are what an operator reaches
			// for next, and a report that says nothing when everything
			// passes is not worth reading.
			Name:   "microvm sandbox",
			Status: OK,
			Detail: fmt.Sprintf("the VMM sandbox is running as pid %d and the guest holds %s", sandboxPID, addr),
		},
		wiringCheck(w, addr),
		guardCheck(w, addr),
	}
}

// wiringCheck reports whether the wire behind the veth is whole.
func wiringCheck(w gateway.VMWiring, addr string) Check {
	if w.OK() {
		return Check{
			Name:   "microvm wiring",
			Status: OK,
			Detail: fmt.Sprintf("eth0 and tap0 are both ports of br0, and the guest holds %s", addr),
		}
	}

	// The tap is called out on its own because it is the failure that
	// looks like success. Firecracker creates a tap of its own when the
	// one qubesome made is absent, and that one is a port of nothing: the
	// guest boots, brings its interface up, takes its address, and reaches
	// nothing at all.
	if w.BridgeUp && w.VethEnslaved && !w.TapEnslaved {
		return Check{
			Name:   "microvm wiring",
			Status: Fail,
			Detail: "tap0 is not a port of br0, so the guest is attached to nothing",
			Fix: "Stop the workload and start it again. If it comes back, the tap is being made after the VMM " +
				"starts rather than before it, and firecracker has created one of its own.",
		}
	}

	return Check{
		Name:   "microvm wiring",
		Status: Fail,
		Detail: fmt.Sprintf("the microVM's bridge is incomplete: br0 up=%t, eth0 enslaved=%t, tap0 enslaved=%t",
			w.BridgeUp, w.VethEnslaved, w.TapEnslaved),
		Fix: "Stop the workload and start it again.",
	}
}

// guardCheck reports on what pins the guest's address.
func guardCheck(w gateway.VMWiring, addr string) Check {
	if !w.Guarded {
		return Check{
			Name:   "microvm guard",
			Status: Fail,
			Detail: "no guard is loaded, so nothing pins the guest's source address",
			Fix: "Stop the workload and start it again. A launch that cannot load the guard is supposed to " +
				"refuse to boot the machine, so a running machine without one should not be possible.",
		}
	}

	if w.Spoofed > 0 {
		return Check{
			Name:   "microvm guard",
			Status: Warn,
			Detail: fmt.Sprintf("the guard has dropped %d frames that did not come from %s", w.Spoofed, addr),
			Fix: "Nothing, unless this is unexpected. The guard is doing its job, and a guest renumbering its " +
				"own interface is what this counts.",
		}
	}

	return Check{
		Name:   "microvm guard",
		Status: OK,
		Detail: fmt.Sprintf("the guest's source address is pinned to %s and nothing has been dropped", addr),
	}
}
