package gateway

import (
	"context"
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

	return &Attach{gateway: g, config: *cfg.Gateway, Addr: addr}, nil
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

	return c.Register(context.Background(), name, a.Addr.String())
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
