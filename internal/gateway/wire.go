package gateway

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
)

const (
	// ipCommand and nsenterCommand are the gateway image's iproute2 and
	// util-linux, which is where every tool that touches the wire comes
	// from. The host needs none of them installed.
	//
	// They are named in full because a helper starts with no environment at
	// all, so there is no PATH for bwrap to search. The image is usr-merged,
	// so /usr/sbin is where iproute2 actually puts ip rather than a symlink
	// to somewhere else.
	ipCommand      = "/usr/sbin/ip"
	nsenterCommand = "/usr/bin/nsenter"

	// workloadLink is the workload's end of the veth, inside the workload's
	// own network namespace. That namespace holds this interface and
	// loopback and nothing else, so the name can be the conventional one.
	workloadLink = "eth0"

	// linkPrefix begins the name of the gateway's end. The gateway holds one
	// end per workload in a single namespace, so those names have to differ
	// from one another.
	linkPrefix = "qw"

	// resolvConfPath is where a workload's resolver configuration goes,
	// inside the workload's own root.
	resolvConfPath = "/etc/resolv.conf"

	// hostBits addresses one host and nothing else. Both ends of every veth
	// take it, for the reason gatewayScript gives.
	hostBits = "/32"
)

// ErrNoGateway reports that no gateway is running for this session, so
// there is nothing to wire a workload to.
var ErrNoGateway = errors.New("gateway: no gateway is running")

// Wire gives a workload's sandbox a link to the session's gateway.
//
// Both ends are created and configured from outside the workload, and the
// workload keeps --cap-drop ALL over its own network namespace. That is the
// security property of this whole stage rather than an incidental tidiness:
// a workload's address is its identity to the gateway, so a workload that
// could renumber itself could claim another workload's policy and another
// workload's injected credentials.
//
// Nothing here has to be undone. The kernel takes a veth with it when the
// network namespace holding an end goes, and the workload's resolver
// configuration is written into a tmpfs overlay that goes at the same time.
//
// addr is the address allocated for this workload and workloadPID is its
// sandbox, in the host's pid namespace, which is what bwrap reports on its
// info descriptor.
func (g Gateway) Wire(cfg types.GatewayConfig, addr netip.Addr, workloadPID int) error {
	w, err := g.wiring(cfg, addr, workloadPID)
	if err != nil {
		return err
	}

	w.starting("workload")

	// One command places both ends, so nothing has to enter a namespace to
	// build the pair. Entering is only needed to address one.
	//
	// OwnNet because the request is authorised against the namespace the
	// caller stands in, not the ones the ends are bound for. In the
	// host's it needs CAP_NET_ADMIN over the host's network, which an
	// ordinary user does not have, and creating the pair failed with
	// RTNETLINK answers: Operation not permitted while both destinations
	// were perfectly reachable.
	if err := (helper{Rootfs: w.rootfs, Caps: wireCaps, Args: linkArgs(w), OwnNet: true}).run(); err != nil {
		return w.failed("create the veth", w.workloadPID, err)
	}

	if err := w.configure(w.gatewayPID, gatewayScript(w)); err != nil {
		return w.failed("configure the gateway end of the veth", w.gatewayPID, err)
	}

	if err := w.configure(w.workloadPID, workloadScript(w)); err != nil {
		return w.failed("configure the workload end of the veth", w.workloadPID, err)
	}

	if err := writeResolvConf(w.workloadPID, w.gatewayAddr); err != nil {
		return err
	}

	slog.Info("[gateway] wired a workload to the gateway",
		"address", addr, "link", w.gatewayLink, "pid", workloadPID)

	return nil
}

// starting says what is about to be wired and to what.
//
// The three pids are the whole of what a wire depends on and none of them
// appear anywhere else. A wire that fails with Operation not permitted
// says nothing about which namespace refused it, and these are what turn
// that into a question somebody can answer.
func (w wiring) starting(kind string) {
	slog.Debug("[gateway] wiring "+kind,
		"address", w.workload,
		"link", w.gatewayLink,
		"gateway", w.gatewayPID,
		"sandbox", w.workloadPID)
}

// failed names the step and the namespace it was working on.
//
// Which step failed is not something the tool's own message says. ip
// reports the same Operation not permitted whether it could not create a
// pair, could not move an end or could not enter a namespace, and the
// three have entirely different causes.
func (w wiring) failed(step string, pid int, err error) error {
	return fmt.Errorf("failed to %s to %s (netns of pid %d, gateway pid %d): %w",
		step, w.workload, pid, w.gatewayPID, err)
}

// wireCaps are what a helper needs to build a wire. CAP_NET_ADMIN moves a
// link into a namespace and addresses it, CAP_SYS_ADMIN enters one. Both are
// held in the session's user namespace, which owns the namespaces both ends
// land in, and neither reaches anything on the host.
var wireCaps = []string{"CAP_NET_ADMIN", "CAP_SYS_ADMIN"}

// wiring is one veth, resolved from the config and the running gateway.
type wiring struct {
	// rootfs is the gateway image, which is where ip and nsenter come from.
	rootfs string

	gatewayAddr netip.Addr
	gatewayPID  int
	gatewayLink string

	workload    netip.Addr
	workloadPID int
}

func (g Gateway) wiring(cfg types.GatewayConfig, addr netip.Addr, workloadPID int) (wiring, error) {
	if workloadPID <= 0 {
		return wiring{}, fmt.Errorf("gateway: workload pid %d is not a process", workloadPID)
	}

	subnet, err := cfg.SubnetPrefix()
	if err != nil {
		return wiring{}, err
	}

	gatewayAddr, err := GatewayAddr(subnet)
	if err != nil {
		return wiring{}, err
	}

	link, err := gatewayLink(subnet, addr)
	if err != nil {
		return wiring{}, err
	}

	gatewayPID, err := g.SandboxPID()
	if err != nil {
		return wiring{}, err
	}

	// The gateway is running, so its image is in the store and this reads
	// the bundle it was started from rather than going to a registry.
	bundle, err := images.PullProfileImage(cfg.Image)
	if err != nil {
		return wiring{}, fmt.Errorf("failed to get the gateway image %q: %w", cfg.Image, err)
	}

	return wiring{
		rootfs:      bundle.Rootfs,
		gatewayAddr: gatewayAddr,
		gatewayPID:  gatewayPID,
		gatewayLink: link,
		workload:    addr,
		workloadPID: workloadPID,
	}, nil
}

// configure runs one namespace's worth of ip commands inside it.
func (w wiring) configure(pid int, script []string) error {
	return w.configureHelper(pid, script).run()
}

// configureHelper is the helper configure runs.
//
// Building it apart from running it is what lets a test read what a step
// is given without a namespace to enter.
func (w wiring) configureHelper(pid int, script []string) helper {
	// No OwnNet here. This one enters the namespace it configures, so the
	// namespace it starts in decides nothing, and giving it one would be
	// a namespace it immediately leaves.
	//
	// No Devices either. Addressing an end and enslaving one are netlink
	// and nothing else, so these steps open no device node. The tap is the
	// exception, and it has a helper of its own.
	return helper{
		Rootfs: w.rootfs,
		Caps:   wireCaps,
		Args:   nsenterArgs(pid),
		Stdin:  strings.NewReader(batch(script)),
	}
}

// linkArgs creates the veth with an end already in each namespace.
//
// One ip link add places both, so neither namespace has to be entered to
// build the pair. The two pids are the host's, for the reason NetnsPath
// gives, and ip resolves them through /proc itself.
func linkArgs(w wiring) []string {
	return []string{
		ipCommand, "link", "add", w.gatewayLink,
		"netns", strconv.Itoa(w.gatewayPID),
		"type", "veth",
		"peer", "name", workloadLink,
		"netns", strconv.Itoa(w.workloadPID),
	}
}

// nsenterArgs runs ip inside one network namespace, reading its commands
// from standard input.
//
// Addressing an end does have to be done from inside, because RTM_NEWADDR
// carries no target namespace. Entering is permitted because the namespace
// is owned by a user namespace nested below the session's, which is where
// the helper's CAP_SYS_ADMIN is held. This was measured on the host rather
// than assumed, as check 8 of hack/verify-sandbox-reentry.sh.
//
// The commands arrive on standard input instead of one invocation each, so
// a namespace is entered once rather than once per command, and nothing
// qubesome computed is ever handed to a shell to parse. ip -batch stops at
// the first line that fails and exits non-zero, which is what makes a
// half-configured end a failure rather than a silence.
func nsenterArgs(pid int) []string {
	return []string{
		nsenterCommand, "--net=" + sandbox.NetnsPath(pid),
		ipCommand, "-batch", "-",
	}
}

// gatewayScript configures the gateway's end.
//
// The address is a /32 and not the subnet's own prefix. The gateway holds
// the same .1 on every veth it has, so anything wider would give it a
// connected route for the whole subnet out of whichever link was added last,
// and one workload's traffic would leave down another workload's wire. The
// host route is what makes each workload reachable, on its own link and no
// other.
//
// There is no masquerade here and no forwarding, because the gateway is not
// a router. Its ruleset redirects 80, 443 and 53 to listeners on the gateway
// itself and drops everything that reaches the forward hook, so it
// terminates connections rather than passing them on, and a terminated
// connection has nothing to translate.
func gatewayScript(w wiring) []string {
	return []string{
		"link set " + w.gatewayLink + " up",
		"addr add " + w.gatewayAddr.String() + hostBits + " dev " + w.gatewayLink,
		"route add " + w.workload.String() + hostBits + " dev " + w.gatewayLink,
	}
}

// workloadScript configures the workload's end.
//
// The address is a /32 for the same reason the gateway's is: the subnet is
// not a shared link. Each workload has a point to point wire to the gateway
// and no path at all to a sibling, so a prefix saying otherwise would only
// invite the kernel to try.
//
// The default route is onlink because a veth has no carrier until both ends
// are up, and whether the connected route has appeared yet is not a thing
// the default route should depend on. onlink says the next hop is on that
// link, which is the one fact about this wire that cannot change.
//
// Loopback comes up here rather than inside the workload, along with
// everything else, because the workload holds nothing over its own network
// namespace and cannot bring up an interface at all.
func workloadScript(w wiring) []string {
	return []string{
		"link set lo up",
		"link set " + workloadLink + " up",
		"addr add " + w.workload.String() + hostBits + " dev " + workloadLink,
		"route add default via " + w.gatewayAddr.String() + " dev " + workloadLink + " onlink",
	}
}

// batch renders a script for ip -batch, which reads one command per line.
func batch(script []string) string {
	return strings.Join(script, "\n") + "\n"
}

// gatewayLink names the gateway's end of the veth to addr.
//
// It is the address's offset into the subnet, so for the /24 a deployment
// usually writes it is the last octet, and qw2 in the gateway is the link to
// 10.111.0.2. An interface name is limited to 15 characters and the widest
// subnet the config allows leaves this well inside that.
func gatewayLink(subnet netip.Prefix, addr netip.Addr) (string, error) {
	offset, err := offsetOf(subnet, addr)
	if err != nil {
		return "", err
	}

	return linkPrefix + strconv.FormatUint(offset, 10), nil
}

// offsetOf returns how far addr is into subnet.
func offsetOf(subnet netip.Prefix, addr netip.Addr) (uint64, error) {
	// Contains answers false for an address of another family, so this also
	// says that both are IPv4 by the time As4 is called on either.
	if !subnet.Contains(addr) {
		return 0, fmt.Errorf("address %s is outside the gateway subnet %s", addr, subnet)
	}

	a := addr.As4()
	n := subnet.Masked().Addr().As4()

	return uint64(binary.BigEndian.Uint32(a[:]) - binary.BigEndian.Uint32(n[:])), nil
}

// writeResolvConf points the workload's resolver at the gateway.
//
// It is written through /proc/<pid>/root, which resolves inside the
// workload's own mount namespace, absolute symlinks included, so nothing has
// to enter that namespace to leave a file in it. What it lands in is the
// sandbox's tmpfs overlay, so the file goes when the sandbox does, which is
// the right lifetime for one naming an address handed out for a single
// launch.
//
// Anything already at that path is removed first. An image is free to ship a
// symlink there, to a systemd runtime directory that a sandbox has nothing
// running in, and the point is to leave a file the resolver will read rather
// than to follow a link elsewhere.
func writeResolvConf(pid int, addr netip.Addr) error {
	if err := replace(resolvConfIn(pid), resolvConf(addr)); err != nil {
		return fmt.Errorf("failed to write the workload's %s: %w", resolvConfPath, err)
	}

	return nil
}

// resolvConfIn names a workload's resolver configuration from outside the
// workload.
func resolvConfIn(pid int) string {
	return sandbox.RootPath(pid) + resolvConfPath
}

// replace writes content at path, whatever was there before.
func replace(path, content string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// 0644 rather than the run directory's 0600, because a resolver library
	// reads it as whatever uid the workload is running as by then, which is
	// not necessarily the one the sandbox started with.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()

		return err
	}

	// Returned rather than deferred away. The last of a write reaches the
	// filesystem at close, so a failure there is a resolv.conf that is
	// not what it says it is, and a workload would come up pointed at
	// nothing with nobody told.
	return f.Close()
}

// resolvConf is the whole of a workload's resolver configuration.
//
// One nameserver and nothing else. The gateway answers on .1 of the subnet,
// on port 53, which its own ruleset redirects to the resolver's port because
// resolv.conf has no way to name one.
func resolvConf(addr netip.Addr) string {
	return "nameserver " + addr.String() + "\n"
}
