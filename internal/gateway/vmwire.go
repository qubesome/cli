package gateway

import (
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
)

// GuestMAC is the hardware address a microVM's interface is given.
//
// It is derived from the workload's address rather than generated, so that
// the host can write it into the machine description and the guard can pin
// it without either end having to read it back off a running interface.
// Two workloads never hold one address, so two guests never hold one MAC.
//
// 02 is locally administered and unicast: bit 1 of the first octet set and
// bit 0 clear. A universally administered address could collide with real
// hardware, and a multicast one is not an interface's identity at all. The
// remaining four octets are the address itself, which makes the sender of a
// frame legible in a capture without a lookup.
func GuestMAC(addr netip.Addr) (string, error) {
	if !addr.Is4() {
		return "", fmt.Errorf("gateway: %s is not an IPv4 address, so it has no guest MAC", addr)
	}

	a := addr.As4()

	return fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", a[0], a[1], a[2], a[3]), nil
}

const (
	// vmBridge joins the veth end to the tap.
	//
	// It holds no address, no route and no forwarding flag. The fc
	// namespace is a wire and not a router, so the guest sits directly on
	// the gateway's link and the gateway's view of a microVM is the same
	// as its view of a sandbox: one address, one host route, one ARP.
	//
	// Routing it instead was considered and rejected. A router in the
	// middle needs forwarding, a route each way and proxy ARP in both
	// directions, and every one of its failures is a silent drop. See the
	// design document.
	vmBridge = "br0"

	// vmTap is the device firecracker opens, and VMTapDevice is the same
	// string for the machine description.
	//
	// Firecracker creates a device of this name when it is absent, and
	// that one is a port of nothing. It gives a guest that boots cleanly,
	// brings its interface up, takes its address and reaches nothing at
	// all, which is the one failure of this topology that looks like
	// success. It is why the tap is made before the VMM starts and why
	// doctor checks that it is a bridge port.
	vmTap = "tap0"

	// vmTapOwner is the uid the tap is handed to, inside the fc sandbox's
	// own user namespace. The sandbox runs as 0 there, which is not host
	// root.
	//
	// Handing it over by uid is what lets firecracker attach holding no
	// capability at all, and that is what keeps a process that escaped the
	// machine from dissolving the bridge or unloading the guard. Measured
	// as checks 9 and 10 of hack/verify-sandbox-reentry.sh.
	vmTapOwner = "0"
)

// VMTapDevice is the tap firecracker is told to open. It is exported
// because the machine description has to name the same string the wiring
// created, and the two must move together.
const VMTapDevice = vmTap

// vmWorkloadScript configures the workload end of a microVM's veth.
//
// It is the counterpart of workloadScript and deliberately shares nothing
// with it. A sandbox's end is addressed. A machine's end is a bridge port,
// because the address belongs to the guest behind it, and a guest that is
// on the gateway's link needs nothing in front of it holding one too.
//
// Loopback comes up here for workloadScript's reason: nothing inside the
// sandbox holds anything over its own network namespace, so it cannot
// bring up an interface at all.
func vmWorkloadScript() []string {
	return []string{
		"link set lo up",
		"link add name " + vmBridge + " type bridge",
		"link set " + vmBridge + " up",
		"link set " + workloadLink + " master " + vmBridge,
		"link set " + workloadLink + " up",
	}
}

// vmTapScript creates the tap and makes it the bridge's second port.
//
// It is a script of its own rather than more lines of the one above
// because the order matters across a process boundary. This has to have
// run before firecracker starts, and the veth has to exist before the
// bridge it is enslaved to carries anything.
func vmTapScript() []string {
	return []string{
		"tuntap add " + vmTap + " mode tap user " + vmTapOwner + " group " + vmTapOwner,
		"link set " + vmTap + " master " + vmBridge,
		"link set " + vmTap + " up",
	}
}

const (
	// nftCommand is the gateway image's nftables.
	//
	// The gateway process runs it to program its own ruleset, and qubesome
	// runs it out of the same rootfs to load a microVM's guard, so a host
	// with no network tooling of its own can still police a machine.
	nftCommand = "/usr/sbin/nft"

	// guardTable is the table the guard is loaded as. It is named so that
	// doctor can ask for it back, and so that loading it a second time
	// replaces it rather than appending to it.
	guardTable = "qubesome"
)

// guardRuleset is the whole of what pins a guest's identity.
//
// A sandbox keeps --cap-drop ALL over its network namespace and therefore
// cannot renumber itself, which is what makes its address its identity
// rather than its choice. A guest is root on a kernel of its own and can
// renumber whatever it likes, so the host has to hold the address down at
// the tap instead. Without this a guest could send as a sibling workload
// and be classified as one. The replies would go to the real holder, so it
// is blind traffic only, but a DNS query is one packet and leaving is the
// whole of its job.
//
// Everything arriving from the gateway side passes untouched, since the
// chain is on the way in and only frames from the tap are the guest's.
// From the tap, only IP carrying the allocated source and ARP claiming the
// allocated sender get through, both pinned to the derived MAC. Everything
// else meets the chain's policy: a renumbered interface, a sibling's
// address, IPv6 solicitations, stray L2.
//
// The counter is named because that is its purpose. It turns a guest
// trying to lie about who it is from invisible into a number doctor
// prints.
func guardRuleset(addr netip.Addr) (string, error) {
	mac, err := GuestMAC(addr)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`table bridge %s {
  counter spoofed {}

  chain ingress {
    type filter hook prerouting priority filter; policy drop;
    iifname != %q accept
    ether saddr %s ip saddr %s accept
    ether saddr %s arp saddr ip %s accept
    counter name spoofed
  }
}
`, guardTable, vmTap, mac, addr, mac, addr), nil
}

// WireVM gives a microVM's sandbox a link to the session's gateway.
//
// It is Wire with a different workload end and two steps Wire has no need
// of. The veth and the gateway's own end are identical, because the
// gateway's view of a microVM is the same as its view of a sandbox: one
// address, one host route, one link. What differs is behind the veth,
// where the end is a bridge port rather than an address, a tap is the
// bridge's second port, and a guard holds the guest to the address it was
// given.
//
// There is no resolv.conf here. A sandbox's is written through
// /proc/<pid>/root into a tmpfs overlay, and a guest's filesystem is
// inside the machine where the host cannot reach it, so a machine is told
// its resolver through the init configuration composed into its image.
//
// The order is load bearing. The tap has to exist and be a bridge port
// before firecracker starts, because firecracker opens the tap by name and
// creates an unenslaved one of its own when the name is absent.
//
// Nothing here has to be undone, for Wire's reason: the kernel takes the
// bridge, the tap and the veth with the network namespace when the sandbox
// holding it goes.
func (g Gateway) WireVM(cfg types.GatewayConfig, addr netip.Addr, sandboxPID int) error {
	w, err := g.wiring(cfg, addr, sandboxPID)
	if err != nil {
		return err
	}

	// OwnNet for the reason Wire gives: a netlink request is authorised
	// against the namespace the caller stands in, not the ones the ends
	// are bound for.
	if err := (helper{Rootfs: w.rootfs, Caps: wireCaps, Args: linkArgs(w), OwnNet: true}).run(); err != nil {
		return fmt.Errorf("failed to create the veth to microVM %s: %w", addr, err)
	}

	if err := w.configure(w.gatewayPID, gatewayScript(w)); err != nil {
		return fmt.Errorf("failed to configure the gateway end of the veth to %s: %w", addr, err)
	}

	if err := w.configure(w.workloadPID, vmWorkloadScript()); err != nil {
		return fmt.Errorf("failed to bridge the microVM end of the veth to %s: %w", addr, err)
	}

	if err := w.configure(w.workloadPID, vmTapScript()); err != nil {
		return fmt.Errorf("failed to create the tap for microVM %s: %w", addr, err)
	}

	if err := w.guard(addr); err != nil {
		return err
	}

	slog.Debug("[gateway] wired a microVM to the gateway",
		"address", addr, "link", w.gatewayLink, "tap", vmTap, "pid", sandboxPID)

	return nil
}

// guard loads the ruleset into the microVM's network namespace.
//
// A guard that will not load fails the launch, which is the rule the rest
// of this package follows: a workload whose policy is not being applied to
// it must not run. Here the address is the policy, so a machine with no
// guard is a machine whose classification anything inside it can choose.
//
// The ruleset arrives on standard input rather than through a file, for
// the reason nsenterArgs gives about ip -batch: nothing qubesome computed
// is handed to a shell or left anywhere a second process could read or
// replace it between the write and the load.
func (w wiring) guard(addr netip.Addr) error {
	rules, err := guardRuleset(addr)
	if err != nil {
		return err
	}

	err = helper{
		Rootfs: w.rootfs,
		Caps:   wireCaps,
		Args: []string{
			nsenterCommand, "--net=" + sandbox.NetnsPath(w.workloadPID),
			nftCommand, "-f", "-",
		},
		Stdin: strings.NewReader(rules),
	}.run()
	if err != nil {
		return fmt.Errorf("failed to load the guard for microVM %s: %w", addr, err)
	}

	return nil
}

// ImageRootfs is the gateway image's root filesystem.
//
// It is the tree the VMM's sandbox runs in. The gateway is up by the time
// anything asks for this, so the image is already in the store and this
// reads the bundle it was started from rather than going to a registry.
func (a *Attach) ImageRootfs() (string, error) {
	bundle, err := images.PullProfileImage(a.config.Image)
	if err != nil {
		return "", fmt.Errorf("failed to get the gateway image %q: %w", a.config.Image, err)
	}

	return bundle.Rootfs, nil
}
