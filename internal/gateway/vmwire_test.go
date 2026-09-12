package gateway

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The MAC is derived so that both ends agree on it without being told. The
// host writes it into the machine description and the guard pins it, and
// neither has to read it back off a running interface.
func TestGuestMACIsDerivedFromTheAddress(t *testing.T) {
	t.Parallel()

	mac, err := GuestMAC(netip.MustParseAddr("10.111.0.2"))
	require.NoError(t, err)
	assert.Equal(t, "02:00:0a:6f:00:02", mac)
}

// Locally administered and unicast. A universally administered address
// could collide with real hardware, and a multicast one is not an
// interface's identity at all.
func TestGuestMACIsLocallyAdministeredUnicast(t *testing.T) {
	t.Parallel()

	mac, err := GuestMAC(netip.MustParseAddr("10.111.0.255"))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(mac, "02:"), "got %q", mac)
	assert.Equal(t, "02:00:0a:6f:00:ff", mac)
}

// Two workloads never hold one address, so two guests never hold one MAC.
func TestGuestMACDiffersWithTheAddress(t *testing.T) {
	t.Parallel()

	a, err := GuestMAC(netip.MustParseAddr("10.111.0.2"))
	require.NoError(t, err)

	b, err := GuestMAC(netip.MustParseAddr("10.111.0.3"))
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
}

func TestGuestMACRefusesIPv6(t *testing.T) {
	t.Parallel()

	_, err := GuestMAC(netip.MustParseAddr("fd00::2"))
	require.Error(t, err)
}

// The veth end is enslaved rather than addressed. The guest holds the
// address, which is what makes the gateway's view of a microVM the same as
// its view of a sandbox.
func TestVMWorkloadScriptBridgesRatherThanAddresses(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"link set lo up",
		"link add name br0 type bridge",
		"link set br0 up",
		"link set eth0 master br0",
		"link set eth0 up",
	}, vmWorkloadScript())
}

// The tap is created from outside and handed over by uid, because the
// sandbox holds no CAP_NET_ADMIN. Firecracker attaches to it, and it must
// not be left to create one of its own, which would be a port of nothing.
func TestVMTapScriptCreatesAndEnslavesTheTap(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"tuntap add tap0 mode tap user 0 group 0",
		"link set tap0 master br0",
		"link set tap0 up",
	}, vmTapScript())
}

// Nothing in the middle holds an address, a route or a forwarding flag.
// The fc namespace is a wire, and a wire that started addressing things
// would be a router, with every silent drop that implies.
func TestVMScriptsAddressNothing(t *testing.T) {
	t.Parallel()

	for _, line := range append(vmWorkloadScript(), vmTapScript()...) {
		assert.NotContains(t, line, "addr add", "the middle must hold no address")
		assert.NotContains(t, line, "route add", "the middle must hold no route")
	}
}

// The guard is what replaces --cap-drop ALL for a guest, which is root on
// its own kernel and can renumber its interface. Only the allocated
// address and the derived MAC get out of the tap.
func TestGuardPinsTheAddressAndTheMAC(t *testing.T) {
	t.Parallel()

	rules, err := guardRuleset(netip.MustParseAddr("10.111.0.2"))
	require.NoError(t, err)

	assert.Contains(t, rules, "table bridge qubesome")
	assert.Contains(t, rules, `iifname != "tap0" accept`)
	assert.Contains(t, rules, "ether saddr 02:00:0a:6f:00:02 ip saddr 10.111.0.2 accept")
	assert.Contains(t, rules, "ether saddr 02:00:0a:6f:00:02 arp saddr ip 10.111.0.2 accept")
	assert.Contains(t, rules, "policy drop")
	assert.Contains(t, rules, "counter name spoofed")
}

// A sibling's address is the case this exists for. Nothing in the ruleset
// may name one, and the chain's policy is what stops it.
func TestGuardNamesOnlyTheWorkloadsOwnAddress(t *testing.T) {
	t.Parallel()

	rules, err := guardRuleset(netip.MustParseAddr("10.111.0.2"))
	require.NoError(t, err)

	assert.NotContains(t, rules, "10.111.0.3")
	assert.NotContains(t, rules, "policy accept")
}

// A ruleset that cannot be built must not become an empty one. An empty
// nft file loads happily and polices nothing.
func TestGuardRulesetRefusesAnAddressItCannotDerive(t *testing.T) {
	t.Parallel()

	_, err := guardRuleset(netip.MustParseAddr("fd00::2"))
	require.Error(t, err)
}
