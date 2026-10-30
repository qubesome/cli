package sandbox

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/vishvananda/netlink"
)

const (
	// guestLink is the interface firecracker gives the guest. The machine
	// description names it eth0 and the guest kernel brings the virtio
	// device up under that name, so the two move together.
	guestLink = "eth0"

	// guestResolvConf is where the guest's resolver configuration goes.
	//
	// It is inside the guest's own root, which a machine may write to: a
	// guest's writes land in an ext4 built for this boot and discarded at
	// shutdown. That is also why nothing has to undo it.
	guestResolvConf = "/etc/resolv.conf"
)

// configureGuestNetwork gives the guest the address the gateway allocated
// for it.
//
// An arbitrary OCI image ships no iproute2, and a machine has no runtime
// to run one out of even if it did, so the init does this itself over
// netlink rather than by executing anything the image happens to carry.
//
// The shape mirrors gateway.workloadScript exactly, and for the same
// reasons. A /32, because the subnet is not a shared link: each workload
// has a point to point wire to the gateway and no path at all to a
// sibling, so a wider prefix would only invite the kernel to try reaching
// one directly. A default route that is onlink, because a veth has no
// carrier until both ends are up, and whether the connected route has
// appeared yet is not a thing the default route should depend on.
//
// Nothing here is a security boundary. A guest is root on a kernel of its
// own and can undo every line of this, which is what the guard on the tap
// is for. This is the configuration a cooperating guest wants, not an
// enforcement of it.
//
// A machine with no gateway has no network block and configures nothing.
func configureGuestNetwork(cfg *vmNetwork) error {
	if cfg == nil {
		return nil
	}

	addr, err := netlink.ParseAddr(cfg.Address + "/32")
	if err != nil {
		return fmt.Errorf("sandbox: the guest was given an address it cannot use, %q: %w", cfg.Address, err)
	}

	gw := net.ParseIP(cfg.Gateway)
	if gw == nil {
		return fmt.Errorf("sandbox: the guest was given a gateway it cannot use, %q", cfg.Gateway)
	}

	link, err := netlink.LinkByName(guestLink)
	if err != nil {
		return fmt.Errorf("sandbox: the guest has no %s to configure: %w", guestLink, err)
	}

	// EEXIST is not a failure on either of these. A machine configures its
	// network once, but the address and the route are exactly what a retry
	// would find already in place, and refusing then would turn a harmless
	// repeat into a machine that does not boot.
	if err := netlink.AddrAdd(link, addr); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("sandbox: failed to give %s the address %s: %w", guestLink, cfg.Address, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("sandbox: failed to bring %s up: %w", guestLink, err)
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        gw,
		Flags:     int(netlink.FLAG_ONLINK),
	}
	if err := netlink.RouteAdd(route); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("sandbox: failed to route the guest through %s: %w", cfg.Gateway, err)
	}

	if err := writeGuestResolvConf(guestResolvConf, cfg.Gateway); err != nil {
		return err
	}

	slog.Info("the guest is on the gateway", "address", cfg.Address, "gateway", cfg.Gateway)

	return nil
}

// writeGuestResolvConf points the guest's resolver at the gateway.
//
// One nameserver and nothing else, which is what the bwrap side writes
// into a sandbox. The gateway answers on its own address, on port 53,
// which its ruleset redirects to the resolver's own port because
// resolv.conf has no way to name one.
//
// Anything already at the path is removed rather than written through. An
// image is free to ship a symlink there, to a systemd runtime directory a
// machine has nothing running in, and the point is to leave a file the
// resolver will read rather than to follow a link elsewhere.
func writeGuestResolvConf(path, addr string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("sandbox: failed to replace the guest's %s: %w", path, err)
	}

	// 0644 rather than the run directory's 0600, because a resolver library
	// reads it as whatever uid the workload is running as by then, which is
	// not necessarily the one the machine booted with.
	//
	//nolint:gosec // G306: a resolver has to be able to read it.
	if err := os.WriteFile(path, []byte("nameserver "+addr+"\n"), 0o644); err != nil {
		return fmt.Errorf("sandbox: failed to write the guest's %s: %w", path, err)
	}

	return nil
}
