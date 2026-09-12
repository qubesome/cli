package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strconv"

	"github.com/qubesome/cli/internal/types"
)

// Attach is one workload's place on the session gateway.
//
// It carries the address allocated for that workload and the gateway that
// handed it out, so the steps a launch takes after its sandbox exists have
// nothing left to resolve.
type Attach struct {
	gateway Gateway
	config  types.GatewayConfig

	// Addr is the workload's address on the gateway's subnet. It is its
	// identity to the gateway rather than merely where it can be reached,
	// which is why nothing inside the sandbox can change it.
	Addr netip.Addr

	// policy is the policy file the gateway was configured with, resolved
	// against the directory the qubesome config was read from. It is
	// carried only so that a registration the gateway refuses can name
	// the file the answer is in, and it is empty when it could not be
	// resolved.
	policy string
}

// Attached prepares the gateway side of a launch, and returns nil when the
// launch needs no gateway.
//
// It starts the session's gateway, waits for it to report itself ready and
// allocates the workload's address, so by the time it returns the only
// things left are the ones that need a sandbox to exist.
//
// It fails closed, and that is the one behaviour here worth being rigid
// about. A configured gateway that will not start is an error that stops
// the workload. Not a warning, not a degraded launch, not egress without
// rules on it. A workload whose policy says which hosts it may reach must
// never run in a state where that policy is not being applied.
//
// An absent gateway block is not a failure. It means no gateway and no
// egress for anything, which is a supported configuration, so the rule
// binds only those who asked for a gateway. A workload that asks for no
// network is not a failure either: it gets nothing, which is the same
// thing the rule protects.
func Attached(cfg *types.Config, network string) (*Attach, error) {
	if cfg == nil || cfg.Gateway == nil || !types.GatewayNetwork(network) {
		return nil, nil //nolint:nilnil // no gateway and no failure is the ordinary case, and there is nothing to return.
	}

	subnet, err := cfg.Gateway.SubnetPrefix()
	if err != nil {
		return nil, err
	}

	g := Current()
	if err := g.Up(*cfg.Gateway, cfg.RootDir, cfg.Source); err != nil {
		return nil, err
	}

	// After Up and not before it. Starting a gateway resets the count, so
	// an address handed out first would be one the new gateway is about to
	// give away again.
	addr, err := g.Allocate(subnet)
	if err != nil {
		return nil, err
	}

	// Not an error. This is for a message, the gateway is already up and
	// running on whatever this would have named, and failing a launch
	// over the provenance of an error that has not happened would be the
	// wrong trade.
	policy, err := cfg.Gateway.ConfigPath(cfg.RootDir)
	if err != nil {
		slog.Debug("[gateway] cannot resolve the gateway policy path", "error", err)

		policy = ""
	}

	return &Attach{gateway: g, config: *cfg.Gateway, Addr: addr, policy: policy}, nil
}

// ProxyAddr returns the endpoint a workload asks for a tunnel on.
//
// The gateway's own address is already the workload's default route and
// its resolver, so a workload could find it for itself. The port it could
// not, so what it is told is the pair, ready to be used as it is.
func (a *Attach) ProxyAddr() (string, error) {
	subnet, err := a.config.SubnetPrefix()
	if err != nil {
		return "", err
	}

	addr, err := GatewayAddr(subnet)
	if err != nil {
		return "", err
	}

	return net.JoinHostPort(addr.String(), strconv.Itoa(inProxyPort)), nil
}

// Wire gives the sandbox at pid a link to the gateway, addressed at both
// ends and with a resolver pointed at it.
//
// pid is the sandbox's own init process in the host's pid namespace, which
// is what bwrap reports on its info descriptor.
func (a *Attach) Wire(pid int) error {
	return a.gateway.Wire(a.config, a.Addr, pid)
}

// Register tells the gateway which workload the address belongs to.
//
// name is the policy key, EffectiveWorkload.Name. The gateway refuses one
// its loaded policy does not mention rather than handing an address to a
// workload it has no rules for, so a workload missing from the gateway's
// config stops here rather than reaching the network unclassified.
func (a *Attach) Register(name string) error {
	c, err := a.gateway.Client()
	if err != nil {
		return err
	}

	if err := c.Register(context.Background(), name, a.Addr.String()); err != nil {
		return a.refused(name, err)
	}

	return nil
}

// refused says where the answer to a refused registration is.
//
// The gateway's own message names the workload and says it is not in the
// loaded policy, which is true and complete and still leaves the reader
// looking for a file. The policy is not the qubesome config, it is not in
// the profile, and the name it wants is the effective one: the workload
// and the profile joined, which is not what is written at the top of the
// workload's own file. All three are things the launch knows and the
// gateway does not.
//
// It is added to every refusal and not only to that one. What the gateway
// refuses a registration for is its own to decide and may grow, and a
// message that named the policy for one reason and not another would be
// worth less than one that always does.
func (a *Attach) refused(name string, err error) error {
	if a.policy == "" {
		return err
	}

	return fmt.Errorf("%w: the gateway's policy is %s, and it has to name this workload as %q; "+
		"a workload the policy does not name is refused an address rather than let out unclassified",
		err, a.policy, name)
}

// Unregister drops the workload's address from the gateway's map.
//
// It is best effort by design. A qubesome run at a terminal exits before
// its workload does, so the reaper that would call this is often gone and
// no message arrives. What that leaks is one map entry naming an address
// nothing will be handed again, because allocation only ever counts up
// within a session. Correctness rests on that non-reuse and not on this
// arriving, which is why a failure here is reported and nothing more.
func (a *Attach) Unregister(name string) {
	c, err := a.gateway.Client()
	if err != nil {
		slog.Debug("could not reach the gateway to unregister a workload", "workload", name, "error", err)
		return
	}

	if err := c.Unregister(context.Background(), name); err != nil {
		slog.Debug("failed to unregister a workload", "workload", name, "error", err)
	}
}

// WireVM gives the microVM sandbox at pid a bridged link to the gateway, a
// tap for its VMM to open, and the guard that pins its address.
//
// pid is the sandbox's own init process in the host's pid namespace, which
// is what bwrap reports on its info descriptor.
func (a *Attach) WireVM(pid int) error {
	return a.gateway.WireVM(a.config, a.Addr, pid)
}

// GuestMAC is the hardware address the machine description gives the
// guest. The guard pins the same value, so both come from one place.
func (a *Attach) GuestMAC() (string, error) {
	return GuestMAC(a.Addr)
}

// GatewayAddr is the address a guest defaults through and resolves at. A
// sandbox is told it by having its resolv.conf written for it, and a guest
// is told it in the init configuration composed into its image.
func (a *Attach) GatewayAddr() (netip.Addr, error) {
	subnet, err := a.config.SubnetPrefix()
	if err != nil {
		return netip.Addr{}, err
	}

	return GatewayAddr(subnet)
}
