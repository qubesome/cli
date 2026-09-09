// Starting a gateway cannot be tested in the development container. No
// sandbox starts here, because mounting /proc in a new pid namespace is
// denied, and the session's namespace holder cannot start for the same
// reason. Running the uplink cannot be tested here either: pasta is not
// installed, and there is no host network namespace to give it even if it
// were.
//
// What is covered below is everything that decides whether a gateway is
// started and what a workload is given once one is: the fail-closed path,
// the singleton, a state file left behind by a crash, the address
// allocation, the pid bwrap reports the sandbox on, and the arguments the
// uplink is run with. Task 12 covers the rest on the target host.
package gateway

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

// The descriptors are numbered by the order of exec.Cmd.ExtraFiles, so the
// order here is part of the launch and not a detail of it.
func TestTheSandboxDescriptorsAreNumberedInOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []int{3, 4, 5, 6}, []int{seccompFD, packedFD, usernsFD, infoFD})
}

func TestNetnsPathNamesTheSandboxNamespace(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/proc/4242/ns/net", NetnsPath(4242))
}

// The pid is bwrap's to report. Between this process and the gateway sit two
// bwraps and a sandbox init, so which pid a veth has to be put next to is
// not something to infer from parentage.
func TestChildPIDIsWhatBwrapReported(t *testing.T) {
	t.Parallel()

	pid, err := childPID(reported(t, `{"child-pid": 4242}`), testTimeout)

	require.NoError(t, err)
	assert.Equal(t, 4242, pid)
}

// bwrap writes more than one field and the object it writes is what ends the
// read, not the end of the descriptor.
func TestChildPIDReadsOneObjectAndStops(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	// The write end stays open, the way the outer bwrap keeps its copy for
	// as long as the sandbox runs. Nothing here ends the read but the
	// object itself.
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	_, err = w.WriteString(`{"child-pid": 4242, "unexpected": "field"}`)
	require.NoError(t, err)

	pid, err := childPID(r, testTimeout)

	require.NoError(t, err)
	assert.Equal(t, 4242, pid)
}

func TestChildPIDFailsWhenNoPidWasReported(t *testing.T) {
	t.Parallel()

	_, err := childPID(reported(t, `{}`), testTimeout)

	require.ErrorContains(t, err, "no pid")
}

func TestChildPIDFailsOnSomethingThatIsNotAnInfoObject(t *testing.T) {
	t.Parallel()

	_, err := childPID(reported(t, "bwrap: execvp gateway: No such file\n"), testTimeout)

	require.ErrorContains(t, err, "failed to read the gateway sandbox pid")
}

// A sandbox that dies before it reports anything leaves a descriptor the
// outer bwrap still holds open, so the read has to give up on its own rather
// than wait for an end of file that is not coming.
func TestChildPIDGivesUpWhenNothingIsReported(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	_, err = childPID(r, time.Millisecond)

	require.ErrorContains(t, err, "failed to read the gateway sandbox pid")
}

// The info descriptor is prefixed to the sandbox's own options, where it
// travels in the packed file and cannot disturb the split PackArgs makes by
// counting back from the end of the list.
func TestTheInfoDescriptorIsPackedWithTheOtherOptions(t *testing.T) {
	t.Parallel()

	spec := sandbox.Spec{Rootfs: t.TempDir(), Args: []string{gatewayCommand}}

	args, err := sandbox.Args(spec, -1)
	require.NoError(t, err)

	outer, packed, err := sandbox.PackArgs(spec, infoFDArgs(args), packedFD)
	require.NoError(t, err)
	defer packed.Close()

	assert.Equal(t, []string{"--args", "4", "--", gatewayCommand}, outer)

	opts, err := io.ReadAll(packed)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(opts), "--info-fd\x006\x00"), "packed options: %q", opts)
}

// pasta runs from the gateway image's rootfs in the session's user
// namespace, and it is pointed at the gateway's network namespace rather
// than confined to it.
func TestTheUplinkRunsPastaAgainstTheGatewayNamespace(t *testing.T) {
	t.Parallel()

	args, err := helper{
		Rootfs:  "/images/gateway/rootfs",
		Caps:    []string{"CAP_NET_ADMIN", "CAP_SYS_ADMIN"},
		Devices: []string{tunDevice},
		Args:    pastaArgs(4242),
	}.bwrapArgs(helperUsernsFD)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"--userns", "3",
		"--overlay-src", "/images/gateway/rootfs",
		"--tmp-overlay", "/",
		"--unshare-ipc",
		"--unshare-uts",
		"--unshare-cgroup",
		"--cap-drop", "ALL",
		"--cap-add", "CAP_NET_ADMIN",
		"--cap-add", "CAP_SYS_ADMIN",
		"--clearenv",
		"--ro-bind", "/proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--new-session",
		"--dev-bind", "/dev/net/tun", "/dev/net/tun",
		"--",
		"/usr/bin/pasta",
		"--foreground",
		"--config-net",
		"-t", "none",
		"-u", "none",
		"-T", "none",
		"-U", "none",
		"--netns", "/proc/4242/ns/net",
	}, args)
}

// The absences are the point. A helper that unshared a network namespace
// would hold pasta's sockets in an empty one and have nothing to serve the
// gateway from. One that unshared a pid namespace could not name the sandbox
// it is pointed at, because its /proc would list only itself. One that
// unshared a user namespace would hold its capabilities beside the
// gateway's namespace rather than above it, and bwrap refuses the
// combination anyway. And the vendored seccomp profile denies setns, which
// is the one thing a helper exists to do.
func TestTheUplinkSharesTheNamespacesItHasToSee(t *testing.T) {
	t.Parallel()

	args, err := uplinkArgs(t)
	require.NoError(t, err)

	for _, unwanted := range []string{"--unshare-net", "--unshare-pid", "--unshare-user", "--seccomp"} {
		assert.NotContains(t, args, unwanted)
	}
}

// The gateway's proxy listeners answer on the address a workload was
// registered under. Published on the host they would answer connections that
// carry no such address, and so no policy.
func TestTheUplinkForwardsNoPorts(t *testing.T) {
	t.Parallel()

	args, err := uplinkArgs(t)
	require.NoError(t, err)

	for _, flag := range []string{"-t", "-u", "-T", "-U"} {
		i := indexOf(args, flag)
		require.GreaterOrEqual(t, i, 0, "flag %q", flag)
		assert.Equal(t, "none", args[i+1], "flag %q", flag)
	}
}

func TestHelperRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	_, err := helper{Args: []string{pastaCommand}}.bwrapArgs(helperUsernsFD)
	require.ErrorContains(t, err, "rootfs")

	_, err = helper{Rootfs: "/images/gateway/rootfs"}.bwrapArgs(helperUsernsFD)
	require.ErrorContains(t, err, "command")

	_, err = helper{
		Rootfs: "/images/gateway/rootfs",
		Caps:   []string{"NET_ADMIN"},
		Args:   []string{pastaCommand},
	}.bwrapArgs(helperUsernsFD)
	require.ErrorContains(t, err, "CAP_ prefix")

	_, err = helper{
		Rootfs: "/images/gateway/rootfs",
		Args:   []string{pastaCommand},
	}.bwrapArgs(2)
	require.ErrorContains(t, err, "standard streams")
}

func uplinkArgs(t *testing.T) ([]string, error) {
	t.Helper()

	return helper{
		Rootfs:  "/images/gateway/rootfs",
		Caps:    []string{"CAP_NET_ADMIN", "CAP_SYS_ADMIN"},
		Devices: []string{tunDevice},
		Args:    pastaArgs(4242),
	}.bwrapArgs(helperUsernsFD)
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}

	return -1
}

// reported returns the read end of a pipe carrying what bwrap wrote to its
// info descriptor, with the write end already closed.
func reported(t *testing.T, out string) *os.File {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })

	_, err = w.WriteString(out)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return r
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
