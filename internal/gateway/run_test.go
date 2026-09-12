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
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/session"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/execabs"
)

const testSubnet = "10.111.0.0/24"

// The absence of a gateway block is a supported configuration and not a
// missing one. It means no gateway and no egress, which is what a sandbox
// without one already had.
func TestAttachedDoesNothingWithoutAGatewayBlock(t *testing.T) {
	t.Parallel()

	att, err := Attached(nil, "qubesome")
	require.NoError(t, err)
	assert.Nil(t, att)

	att, err = Attached(&types.Config{}, "qubesome")
	require.NoError(t, err)
	assert.Nil(t, att)
}

// A workload that asks for no network is given none, which is the outcome the
// fail-closed rule protects rather than one it has to prevent. Starting a
// gateway for it would be a process nothing talks to.
func TestAttachedDoesNothingForAWorkloadWithNoNetwork(t *testing.T) {
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
		att, err := Attached(cfg, network)
		require.NoError(t, err, "network %q", network)
		assert.Nil(t, att, "network %q", network)
	}
}

// The rule the whole stage rests on. The gateway here cannot start, because
// the policy file it names is not there, and the launch has to stop rather
// than carry on with no policy applied to it.
func TestAttachedStopsTheLaunchWhenTheGatewayWillNotStart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &types.Config{
		RootDir: t.TempDir(),
		Gateway: &types.GatewayConfig{
			Image:  "ghcr.io/qubesome/gateway:latest",
			Config: "/gateway.yml",
			Subnet: testSubnet,
		},
	}

	_, err := Attached(cfg, "qubesome")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.yml")
}

// One gateway per session. The config here would fail at the first thing
// start does, so a launch that returns without an error is one that never
// went looking for a second gateway to start. The gateway it found instead is
// asked to re-read its policy, because the file may have been edited since the
// launch that started it read it.
func TestUpDoesNotStartASecondGatewayWhenOneIsRunning(t *testing.T) {
	g := newSessionGateway(t)

	creds := newCreds(t)
	require.NoError(t, g.writeCreds(creds))
	gw := newGateway(closedChan())
	listenOn(t, gw, creds, g.Socket)

	runningGateway(t, g)

	require.NoError(t, g.Up(unusableConfig(), t.TempDir(), ""))

	assert.Equal(t, 1, gw.reloaded())
}

// A gateway too old to re-read its policy is one that behaves as every gateway
// did before the call existed, so a launch carries on rather than refusing to
// start a workload over it.
func TestUpAcceptsAGatewayThatCannotReload(t *testing.T) {
	g := newSessionGateway(t)

	creds := newCreds(t)
	require.NoError(t, g.writeCreds(creds))
	gw := newGateway(closedChan())
	gw.noReload = true
	listenOn(t, gw, creds, g.Socket)

	runningGateway(t, g)

	assert.NoError(t, g.Up(unusableConfig(), t.TempDir(), ""))
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

	err := g.Up(unusableConfig(), t.TempDir(), "")

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

// The info descriptor is prefixed to the sandbox's own options, where it
// travels in the packed file and cannot disturb the split PackArgs makes by
// counting back from the end of the list.
func TestTheInfoDescriptorIsPackedWithTheOtherOptions(t *testing.T) {
	t.Parallel()

	spec := sandbox.Spec{Rootfs: t.TempDir(), Args: []string{gatewayCommand}}

	args, err := sandbox.Args(spec, -1)
	require.NoError(t, err)

	outer, packed, err := sandbox.PackArgs(spec, sandbox.InfoFDArgs(args, infoFD), packedFD)
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
		Session: session.Session{
			Dir:       dir,
			LockPath:  filepath.Join(dir, "session.lock"),
			StatePath: filepath.Join(dir, "holder.json"),
		},
	}
}

// runningGateway records a gateway this session can use: a process that
// is alive, and the user namespace of a holder that is running.
//
// Both halves are needed. A gateway is only this session's gateway when
// the namespace it was started in is the one being held, and the test
// process stands in for both the holder and the gateway because it is a
// live process in a namespace it can name.
func runningGateway(t *testing.T, g Gateway) {
	t.Helper()

	require.NoError(t, sandbox.WriteState(g.Session.StatePath, os.Getpid()))

	held, err := g.Session.UsernsID()
	require.NoError(t, err)

	require.NoError(t, sandbox.WriteStateSession(g.StatePath, os.Getpid(), held))
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

// A subnet changed under a session that has already handed addresses out
// is refused, because the count belongs to the old range and a gateway in
// the session holds its first address.
//
// The message opens the way the status one does, so the two describe the
// same thing in the same words. It keeps the claim about a running
// gateway that the status message drops: Allocate is only ever reached
// after Up, so by here there is one, and it is why a restart is the
// remedy rather than an edit.
func TestAllocateRefusesAChangedSubnet(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	_, err := g.Allocate(prefix(t, testSubnet))
	require.NoError(t, err)

	_, err = g.Allocate(prefix(t, "10.112.0.0/24"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "this session has handed addresses out of 10.111.0.0/24")
	assert.Contains(t, err.Error(), "the config now asks for 10.112.0.0/24")
	assert.Contains(t, err.Error(), "the session has to be restarted")
}

// A gateway is started inside the session holder's user namespace, so a
// gateway whose namespace is not the one being held now is one the
// session cannot wire anything to. Its pid says nothing about that.
func TestAGatewayOfAnotherSessionIsStranded(t *testing.T) {
	t.Parallel()

	err := strandedBy(
		sandbox.State{PID: 42, Session: 4026531837},
		sandbox.State{PID: 7, StartTime: 100},
		4026532200, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "4026531837")
	assert.Contains(t, err.Error(), "4026532200")
}

// The namespace being held is the one the gateway was started in, so
// there is nothing wrong with it.
func TestAGatewayOfThisSessionIsNotStranded(t *testing.T) {
	t.Parallel()

	assert.NoError(t, strandedBy(
		sandbox.State{PID: 42, Session: 4026532200},
		sandbox.State{PID: 7, StartTime: 100},
		4026532200, nil))
}

// A gateway was started inside some holder's namespace, so one running
// with no holder anywhere is one whose holder has gone.
func TestAGatewayWithNoHolderIsStranded(t *testing.T) {
	t.Parallel()

	err := strandedBy(
		sandbox.State{PID: 42, Session: 4026532200},
		sandbox.State{},
		0, session.ErrNoHolder)

	require.Error(t, err)
	assert.ErrorIs(t, err, session.ErrNoHolder)
}

// A gateway a previous release started records no namespace. Only one
// holder runs at a time and a gateway is always started inside the one
// that is running, so a gateway that started after this holder did is
// this holder's gateway however little its record says. Killing it would
// take the egress from a session that was working.
func TestAGatewayOlderThanTheRecordButNewerThanTheHolderIsNotStranded(t *testing.T) {
	t.Parallel()

	assert.NoError(t, strandedBy(
		sandbox.State{PID: 42, StartTime: 200},
		sandbox.State{PID: 7, StartTime: 100},
		4026532200, nil))
}

// One that started before this holder did belongs to a holder that has
// since gone, whatever its pid says.
func TestAGatewayOlderThanTheHolderIsStranded(t *testing.T) {
	t.Parallel()

	err := strandedBy(
		sandbox.State{PID: 42, StartTime: 100},
		sandbox.State{PID: 7, StartTime: 200},
		4026532200, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "started before this session's holder")
}

// With no holder record there is nothing to date it against, and a
// gateway that cannot be shown to be this session's is not treated as
// one.
func TestAGatewayWithNoSessionAndNoHolderRecordIsStranded(t *testing.T) {
	t.Parallel()

	assert.Error(t, strandedBy(sandbox.State{PID: 42, StartTime: 200}, sandbox.State{}, 4026532200, nil))
}

// A gateway of a session that has gone is alive, so nothing about its pid
// says it cannot be used. It is taken down and a fresh one is started,
// which here is the start path failing on the unusable config.
func TestUpReplacesAGatewayOfASessionThatHasGone(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := execabs.CommandContext(ctx, "sleep", "60")
	require.NoError(t, cmd.Start())
	defer func() { _ = cmd.Wait() }()

	// A holder is running, and the gateway names a namespace that is not
	// the one it is holding. The gateway is the younger of the two
	// processes, so it is the recorded namespace and nothing else that
	// makes this one stranded.
	require.NoError(t, sandbox.WriteState(g.Session.StatePath, os.Getpid()))
	require.NoError(t, sandbox.WriteStateSession(g.StatePath, cmd.Process.Pid, 1))

	err := g.Up(unusableConfig(), t.TempDir(), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway.yml", "the start path must have been taken")
	assert.NoFileExists(t, g.StatePath, "the stranded gateway's record must not be left behind")
}

// Nothing wires to a gateway of a session that has gone. The veth would
// be refused by the kernel with a message that names nothing, so the
// refusal is here, where it can say what is wrong.
func TestSandboxPIDRefusesAGatewayOfASessionThatHasGone(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	require.NoError(t, sandbox.WriteState(g.Session.StatePath, os.Getpid()))
	require.NoError(t, sandbox.WriteStateSession(g.StatePath, os.Getpid(), 1))

	_, err := g.SandboxPID()

	require.ErrorIs(t, err, ErrNoGateway)
	assert.Contains(t, err.Error(), "a session that has gone")
}

// A profile started by a previous release leaves a gateway whose record
// names no namespace. It is this session's gateway, and a launch from a
// newer binary has to go on using it rather than taking the egress from
// the workloads already running on it.
func TestUpReusesAGatewayFromAReleaseThatRecordedNoSession(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	creds := newCreds(t)
	require.NoError(t, g.writeCreds(creds))
	gw := newGateway(closedChan())
	listenOn(t, gw, creds, g.Socket)

	// The holder started before the gateway did, which is the only order
	// the two can ever be in, and the gateway's record names no
	// namespace, which is what a previous release wrote. The parent
	// process stands in for the holder because it is a live process that
	// started before this one.
	require.NoError(t, sandbox.WriteState(g.Session.StatePath, os.Getppid()))
	require.NoError(t, sandbox.WriteState(g.StatePath, os.Getpid()))

	require.NoError(t, g.Up(unusableConfig(), t.TempDir(), ""))

	assert.Equal(t, 1, gw.reloaded(), "the gateway that was running must have been reused")
}
