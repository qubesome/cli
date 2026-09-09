// None of the wiring can be run in the development container. ip and
// nsenter come from the gateway image and are not installed here, and this
// container cannot create the network namespaces the two ends would go into
// even if they were. What is covered below is everything that decides what
// the commands say: the argument list of each one, the address arithmetic
// against the subnet, what a workload's resolver configuration ends up
// naming, and the paths that stop a launch before any of it runs. Task 12
// covers the rest on the target host.
package gateway

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One command places both ends, so neither namespace has to be entered to
// build the pair.
func TestTheVethIsCreatedWithAnEndAlreadyInEachNamespace(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"/usr/sbin/ip", "link", "add", "qw2",
		"netns", "100",
		"type", "veth",
		"peer", "name", "eth0",
		"netns", "200",
	}, linkArgs(testWiring()))
}

// Addressing an end does have to be done from inside, because RTM_NEWADDR
// carries no target namespace.
func TestAddressingEntersTheNamespaceItIsFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"/usr/bin/nsenter", "--net=/proc/200/ns/net",
		"/usr/sbin/ip", "-batch", "-",
	}, nsenterArgs(200))
}

// The gateway holds the same .1 on every veth it has, so its end is a single
// address and a host route rather than a prefix that would claim the whole
// subnet on whichever link was added last.
func TestTheGatewayEndTakesOneAddressAndOneRoute(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"link set qw2 up",
		"addr add 10.111.0.1/32 dev qw2",
		"route add 10.111.0.2/32 dev qw2",
	}, gatewayScript(testWiring()))
}

// Every one of these is run from outside the workload, which keeps
// --cap-drop ALL over its own network namespace and cannot so much as bring
// its loopback up. Its address is its identity to the gateway, so a workload
// that could renumber itself could claim another workload's policy and
// another workload's injected credentials.
func TestTheWorkloadEndIsConfiguredEntirelyFromOutside(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"link set lo up",
		"link set eth0 up",
		"addr add 10.111.0.2/32 dev eth0",
		"route add default via 10.111.0.1 dev eth0 onlink",
	}, workloadScript(testWiring()))
}

// The gateway is not a router. Its ruleset redirects 80, 443 and 53 to
// listeners on the gateway itself and drops everything reaching the forward
// hook, so it terminates connections rather than passing them on, and a
// terminated connection has nothing to translate.
func TestNothingIsMasqueradedAndNothingIsForwarded(t *testing.T) {
	t.Parallel()

	w := testWiring()

	lines := append(gatewayScript(w), workloadScript(w)...)
	lines = append(lines, strings.Join(linkArgs(w), " "))

	for _, line := range lines {
		for _, unwanted := range []string{"masquerade", "nat", "snat", "forward", "sysctl", "iptables", "nft"} {
			assert.NotContains(t, line, unwanted, "line %q", line)
		}
	}
}

func TestTheScriptIsOneCommandPerLine(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "link set lo up\nlink set eth0 up\n", batch([]string{"link set lo up", "link set eth0 up"}))
}

// The gateway holds one end per workload in a single namespace, so the names
// have to differ, and the offset into the subnet is both unique and readable
// in the gateway's own ip link output.
func TestTheGatewayEndIsNamedAfterTheAddress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		subnet string
		addr   string
		want   string
	}{
		{"10.111.0.0/24", "10.111.0.2", "qw2"},
		{"10.111.0.0/24", "10.111.0.254", "qw254"},
		{"10.111.0.0/16", "10.111.1.3", "qw259"},
		{"192.168.8.0/30", "192.168.8.2", "qw2"},
	} {
		name, err := gatewayLink(prefix(t, tc.subnet), netip.MustParseAddr(tc.addr))

		require.NoError(t, err)
		assert.Equal(t, tc.want, name)
		assert.LessOrEqual(t, len(name), 15, "an interface name is limited to 15 characters")
	}
}

func TestTheGatewayEndRefusesAnAddressFromAnotherSubnet(t *testing.T) {
	t.Parallel()

	_, err := gatewayLink(prefix(t, testSubnet), netip.MustParseAddr("10.112.0.2"))
	require.ErrorContains(t, err, "outside the gateway subnet")

	_, err = gatewayLink(prefix(t, testSubnet), netip.MustParseAddr("fd00::2"))
	require.ErrorContains(t, err, "outside the gateway subnet")
}

// resolv.conf cannot name a port, which is why the gateway's ruleset
// redirects 53 to the resolver's own port rather than the resolver binding
// the privileged one.
func TestTheWorkloadResolvesThroughTheGateway(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "nameserver 10.111.0.1\n", resolvConf(netip.MustParseAddr("10.111.0.1")))
}

// The file is written through the workload's own root, which resolves inside
// its mount namespace, so nothing has to enter that namespace to leave a
// file in it.
func TestTheResolverConfigurationIsWrittenIntoTheWorkloadsOwnRoot(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/proc/200/root/etc/resolv.conf", resolvConfIn(200))
}

func TestReplaceWritesOverWhatIsThere(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "resolv.conf")
	require.NoError(t, os.WriteFile(path, []byte("nameserver 192.0.2.1\n"), 0o600))

	require.NoError(t, replace(path, "nameserver 10.111.0.1\n"))

	assert.Equal(t, "nameserver 10.111.0.1\n", read(t, path))
}

// An image is free to ship a symlink there, to a runtime directory a sandbox
// has nothing running in. The point is to leave a file the resolver will
// read, not to follow a link somewhere else.
func TestReplaceDoesNotWriteThroughASymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	require.NoError(t, os.WriteFile(target, []byte("untouched"), 0o600))

	path := filepath.Join(dir, "resolv.conf")
	require.NoError(t, os.Symlink(target, path))

	require.NoError(t, replace(path, "nameserver 10.111.0.1\n"))

	assert.Equal(t, "nameserver 10.111.0.1\n", read(t, path))
	assert.Equal(t, "untouched", read(t, target))
}

// The wire is built by a helper that joins the session's user namespace,
// where the capabilities it needs are held and from where they reach both
// namespaces the ends land in. The workload is given none of this.
func TestTheWireIsBuiltFromTheSessionNamespace(t *testing.T) {
	t.Parallel()

	args, err := helper{
		Rootfs: "/images/gateway/rootfs",
		Caps:   wireCaps,
		Args:   linkArgs(testWiring()),
	}.bwrapArgs(helperUsernsFD)

	require.NoError(t, err)
	assert.Subset(t, args, []string{"--userns", "--cap-drop", "ALL", "CAP_NET_ADMIN", "CAP_SYS_ADMIN"})
	assert.NotContains(t, args, "--unshare-user")
	assert.NotContains(t, args, "--unshare-net")
	assert.NotContains(t, args, "--unshare-pid")
}

// Wiring a workload to a gateway that is not running is a workload with no
// policy applied to it, which stops the launch as every other failure in
// this package does.
func TestWireStopsWhenNoGatewayIsRunning(t *testing.T) {
	t.Parallel()

	err := newSessionGateway(t).Wire(unusableConfig(), netip.MustParseAddr("10.111.0.2"), 200)

	require.ErrorIs(t, err, ErrNoGateway)
}

func TestWireRefusesWhatCannotBeWired(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	err := g.Wire(unusableConfig(), netip.MustParseAddr("10.111.0.2"), 0)
	require.ErrorContains(t, err, "not a process")

	err = g.Wire(types.GatewayConfig{Subnet: "10.111.0.0/33"}, netip.MustParseAddr("10.111.0.2"), 200)
	require.ErrorContains(t, err, "invalid gateway subnet")

	err = g.Wire(unusableConfig(), netip.MustParseAddr("10.112.0.2"), 200)
	require.ErrorContains(t, err, "outside the gateway subnet")
}

func testWiring() wiring {
	return wiring{
		rootfs:      "/images/gateway/rootfs",
		gatewayAddr: netip.MustParseAddr("10.111.0.1"),
		gatewayPID:  100,
		gatewayLink: "qw2",
		workload:    netip.MustParseAddr("10.111.0.2"),
		workloadPID: 200,
	}
}

func read(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(data)
}
