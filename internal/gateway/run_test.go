// Starting a gateway cannot be tested in the development container. No
// sandbox starts here, because mounting /proc in a new pid namespace is
// denied, and the session's namespace holder cannot start for the same
// reason. What is covered below is everything that decides whether a gateway
// is started and what a workload is given once one is: the fail-closed path,
// the singleton, a state file left behind by a crash, and the address
// allocation. Task 12 covers the rest on the target host.
package gateway

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSubnet = "10.111.0.0/24"

func TestServesOnlyANamedNetwork(t *testing.T) {
	t.Parallel()

	for _, network := range []string{"", "none", "host"} {
		assert.False(t, Serves(network), "network %q", network)
	}

	assert.True(t, Serves("qubesome"))
}

// The absence of a gateway block is a supported configuration and not a
// missing one. It means no gateway and no egress, which is what a sandbox
// without one already had.
func TestEnsureDoesNothingWithoutAGatewayBlock(t *testing.T) {
	t.Parallel()

	require.NoError(t, Ensure(nil, "qubesome"))
	require.NoError(t, Ensure(&types.Config{}, "qubesome"))
}

// A workload that asks for no network is given none, which is the outcome the
// fail-closed rule protects rather than one it has to prevent. Starting a
// gateway for it would be a process nothing talks to.
func TestEnsureDoesNothingForAWorkloadWithNoNetwork(t *testing.T) {
	t.Parallel()

	cfg := &types.Config{
		RootDir: t.TempDir(),
		Gateway: &types.GatewayConfig{
			Image:  "ghcr.io/qubesome/gateway:latest",
			Config: "/gateway.yml",
			Subnet: testSubnet,
		},
	}

	for _, network := range []string{"", "none", "host"} {
		require.NoError(t, Ensure(cfg, network), "network %q", network)
	}
}

// The rule the whole stage rests on. The gateway here cannot start, because
// the policy file it names is not there, and the launch has to stop rather
// than carry on with no policy applied to it.
func TestEnsureStopsTheLaunchWhenTheGatewayWillNotStart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &types.Config{
		RootDir: t.TempDir(),
		Gateway: &types.GatewayConfig{
			Image:  "ghcr.io/qubesome/gateway:latest",
			Config: "/gateway.yml",
			Subnet: testSubnet,
		},
	}

	err := Ensure(cfg, "qubesome")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.yml")
}

// One gateway per session. The config here would fail at the first thing
// start does, so a launch that returns without an error is one that never
// went looking for a second gateway to start.
func TestUpDoesNotStartASecondGatewayWhenOneIsRunning(t *testing.T) {
	g := newSessionGateway(t)

	creds := newCreds(t)
	require.NoError(t, g.writeCreds(creds))
	listenOn(t, newGateway(closedChan()), creds, g.Socket)

	require.NoError(t, sandbox.WriteState(g.StatePath, os.Getpid()))

	require.NoError(t, g.Up(unusableConfig(), t.TempDir()))
}

// A state file outlives the process it names, so a gateway that crashed must
// read as not running. Here that means the start path is taken, and the
// unusable config is what says so.
func TestUpStartsAGatewayWhenTheStateIsStale(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	// The pid is this process, so it exists, and the start time is one no
	// process has. Alive compares both.
	state := fmt.Sprintf(`{"pid":%d,"startTime":1}`, os.Getpid())
	require.NoError(t, os.WriteFile(g.StatePath, []byte(state), 0o600))

	err := g.Up(unusableConfig(), t.TempDir())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.yml")
}

func TestGatewayAddrIsTheFirstHostAddress(t *testing.T) {
	t.Parallel()

	addr, err := GatewayAddr(prefix(t, testSubnet))

	require.NoError(t, err)
	assert.Equal(t, "10.111.0.1", addr.String())
}

// Workloads take the addresses above the gateway's own, and the count only
// goes up.
func TestAllocateIsMonotonic(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	subnet := prefix(t, testSubnet)

	for _, want := range []string{"10.111.0.2", "10.111.0.3", "10.111.0.4"} {
		addr, err := g.Allocate(subnet)

		require.NoError(t, err)
		assert.Equal(t, want, addr.String())
	}
}

// Each qubesome run is a new process and the gateway outlives all of them, so
// the count is in a file rather than in memory.
func TestAllocateContinuesAcrossARestart(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	subnet := prefix(t, testSubnet)

	_, err := g.Allocate(subnet)
	require.NoError(t, err)

	restarted := g
	addr, err := restarted.Allocate(subnet)

	require.NoError(t, err)
	assert.Equal(t, "10.111.0.3", addr.String())
}

// An address is a workload's identity to the gateway, and Register refuses a
// duplicate, so two launches at once must not read the same count. flock is
// held on the open file description, so it separates two goroutines of one
// process as well as two processes.
func TestAllocateNeverHandsOutOneAddressTwice(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	subnet := prefix(t, testSubnet)

	const launches = 8

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = make(map[string]bool, launches)
	)

	for range launches {
		wg.Add(1)

		go func() {
			defer wg.Done()

			addr, err := g.Allocate(subnet)
			assert.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()
			seen[addr.String()] = true
		}()
	}

	wg.Wait()

	assert.Len(t, seen, launches)
}

// Wrapping round would hand a live workload the address, and the policy, of
// one that is still running.
func TestAllocateReportsAnExhaustedSubnet(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	// A /30 holds a network address, the gateway's, one workload's and a
	// broadcast address.
	subnet := prefix(t, "10.111.0.0/30")

	addr, err := g.Allocate(subnet)
	require.NoError(t, err)
	assert.Equal(t, "10.111.0.2", addr.String())

	_, err = g.Allocate(subnet)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "10.111.0.0/30")
	assert.Contains(t, err.Error(), "all 1 of its addresses")
}

// The running gateway holds the first address of the range it was started
// with, so a subnet changed underneath it is a misconfiguration and not a
// new range to count from.
func TestAllocateRefusesASubnetTheSessionDidNotStartWith(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	_, err := g.Allocate(prefix(t, testSubnet))
	require.NoError(t, err)

	_, err = g.Allocate(prefix(t, "10.112.0.0/24"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), testSubnet)
	assert.Contains(t, err.Error(), "10.112.0.0/24")
}

func TestAllocateRefusesASubnetItCannotHandAddressesOutOf(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	_, err := g.Allocate(prefix(t, "fd00::/64"))
	require.ErrorContains(t, err, "not IPv4")

	_, err = g.Allocate(prefix(t, "10.111.0.0/31"))
	require.ErrorContains(t, err, "no host addresses")
}

// newSessionGateway returns a gateway whose files are all in the test's own
// directory, so nothing here reads or writes the user's session.
func newSessionGateway(t *testing.T) Gateway {
	t.Helper()

	dir := t.TempDir()

	return Gateway{
		Dir:        dir,
		LockPath:   filepath.Join(dir, "gateway.lock"),
		StatePath:  filepath.Join(dir, "sandbox-gateway.json"),
		AllocPath:  filepath.Join(dir, "gateway-addresses.json"),
		CredsPath:  filepath.Join(dir, "gateway-creds.json"),
		Socket:     filepath.Join(dir, "control.sock"),
		SocketDir:  dir,
		SecretsDir: filepath.Join(dir, "secrets"),
	}
}

// unusableConfig names a policy file that is not there, so anything that
// reaches the start path fails at the first thing it does.
func unusableConfig() types.GatewayConfig {
	return types.GatewayConfig{
		Image:  "ghcr.io/qubesome/gateway:latest",
		Config: "/gateway.yml",
		Subnet: testSubnet,
	}
}

func prefix(t *testing.T, s string) netip.Prefix {
	t.Helper()

	p, err := netip.ParsePrefix(s)
	require.NoError(t, err)

	return p
}
