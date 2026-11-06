package bwrap

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/gateway"
	"github.com/qubesome/cli/internal/runners/util/launch"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/execabs"
)

func gatewayInput() input {
	in := plainInput()

	in.Gateway = true
	in.Workload.Workload.HostAccess.Network = "qubesome"
	in.QubesomeBin = "/usr/bin/qubesome"
	in.AgentDir = "/run/user/1000/qubesome/work/agent/chrome"

	return in
}

// A named network used to mean an empty namespace and now means an address
// on the session gateway. none and host keep their meanings, and a named
// network with no gateway keeps the old one.
func TestWorkloadNetMapsANamedNetworkOnlyWithAGateway(t *testing.T) {
	t.Parallel()

	assert.Equal(t, sandbox.NetGateway, workloadNet("qubesome", true))
	assert.Equal(t, sandbox.NetNone, workloadNet("qubesome", false))

	for _, gw := range []bool{false, true} {
		assert.Equal(t, sandbox.NetHost, workloadNet("host", gw))

		for _, network := range []string{"", "none"} {
			assert.Equal(t, sandbox.NetNone, workloadNet(network, gw), "network %q", network)
		}
	}
}

// bwrap is handed the same thing either way. A workload on the gateway gets
// its own empty namespace and the veth is put in it from outside, so the
// difference is in what qubesome does next and not in the sandbox.
func TestSpecOnTheGatewayStillUnsharesTheNetwork(t *testing.T) {
	t.Parallel()

	in := gatewayInput()

	spec, err := buildSpec(in)
	require.NoError(t, err)
	assert.Equal(t, sandbox.NetGateway, spec.Net)

	assert.Contains(t, render(t, in), "--unshare-net")
}

// The sandbox has to exist before its veth can be built, so the workload is
// held by a supervisor in the window between the two.
func TestSpecOnTheGatewayRunsAGatedSupervisor(t *testing.T) {
	t.Parallel()

	args := render(t, gatewayInput())

	i := slices.Index(args, "--")
	require.NotEqual(t, -1, i)

	assert.Equal(t,
		[]string{files.InProfileBinary, sandbox.SuperviseCommand, sandbox.GatedFlag},
		args[i+1:i+4])
}

// A single instance workload with no gateway waits for nothing, so it runs
// the supervisor without the flag.
func TestSpecWithoutTheGatewayRunsAnUngatedSupervisor(t *testing.T) {
	t.Parallel()

	assert.NotContains(t, render(t, supervisedInput()), sandbox.GatedFlag)
}

// The supervisor is the qubesome binary and answers on a socket, so a
// workload on the gateway needs both whether or not it is single instance.
func TestSpecOnTheGatewayNeedsASupervisorToRelease(t *testing.T) {
	t.Parallel()

	in := gatewayInput()
	in.QubesomeBin = ""
	in.AgentDir = ""

	_, err := buildSpec(in)

	require.ErrorContains(t, err, "needs a supervisor")
}

// fakeAttach records the calls a launch makes and can fail any of them, so
// the order the launch takes them in is what is being checked rather than
// what a gateway would have done with them.
type fakeAttach struct {
	calls []string

	wireErr     error
	resolveErr  error
	registerErr error

	// socket, when set, is asked at the moment of the resolve whether a
	// supervisor is answering on it, and the answer kept in upAtResolve.
	socket      string
	upAtResolve bool
}

func (f *fakeAttach) Wire(int) error {
	f.calls = append(f.calls, "wire")

	return f.wireErr
}

func (f *fakeAttach) Resolve(int) error {
	f.calls = append(f.calls, "resolve")
	if f.socket != "" {
		f.upAtResolve = sandbox.Ready(f.socket) == nil
	}

	return f.resolveErr
}

func (f *fakeAttach) Register(string) error {
	f.calls = append(f.calls, "register")

	return f.registerErr
}

func (f *fakeAttach) Unregister(string) {
	f.calls = append(f.calls, "unregister")
}

// gatedLaunch returns a launcher whose sandbox is this process, with a gated
// supervisor answering where the host will look for one.
//
// The pid is set rather than read, since there is no bwrap here to report
// one. What the launch does with it is Wire's business and this fake's.
func gatedLaunch(t *testing.T, ew types.EffectiveWorkload, argv []string) (*launch.Launcher, string) {
	t.Helper()

	dir, err := files.WorkloadAgentDir(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, files.DirMode))

	socket, err := files.WorkloadAgentSocket(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)

	go func() { _ = sandbox.Supervise(socket, argv, true) }()

	return launch.ForTest(os.Getpid()), socket
}

// The whole ordering. The sandbox exists, the veth is built and addressed,
// the gateway is told whose address it is, and only then is the workload
// released into a namespace that is already wired.
func TestAttachWiresAndRegistersBeforeReleasing(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	marker := filepath.Join(t.TempDir(), "ran")

	l, socket := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker + "; sleep 5"})

	att := &fakeAttach{}
	require.NoError(t, attach(l, att, ew, socket))

	assert.Equal(t, []string{"wire", "resolve", "register"}, att.calls)

	require.Eventually(t, func() bool {
		_, err := os.Stat(marker)

		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the release did not start the workload")
}

// A veth that cannot be built stops the launch, and nothing is registered
// for an address the workload cannot be reached on.
func TestAttachStopsWhenTheVethFails(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	marker := filepath.Join(t.TempDir(), "ran")

	l, socket := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker})

	att := &fakeAttach{wireErr: errors.New("no veth")}

	require.ErrorContains(t, attach(l, att, ew, socket), "no veth")
	assert.Equal(t, []string{"wire"}, att.calls)

	assertNeverRan(t, marker)
}

// A gateway that refuses the workload's name has no policy for it, and a
// workload with no policy must not reach the network at all.
func TestAttachStopsWhenRegisterFails(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	marker := filepath.Join(t.TempDir(), "ran")

	l, socket := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker})

	att := &fakeAttach{registerErr: errors.New("unknown workload")}

	require.ErrorContains(t, attach(l, att, ew, socket), "unknown workload")
	assert.Equal(t, []string{"wire", "resolve", "register"}, att.calls)

	assertNeverRan(t, marker)
}

// bwrap reports the sandbox's pid before it has built the sandbox's
// filesystem, so the resolver configuration waits for the supervisor, which
// only runs once that filesystem is in place. Written any earlier, it lands
// in a root that has no /etc yet.
func TestAttachResolvesOnceSupervised(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})

	dir, err := files.WorkloadAgentDir(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, files.DirMode))

	socket, err := files.WorkloadAgentSocket(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)

	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = sandbox.Supervise(socket, []string{"/bin/sh", "-c", "sleep 5"}, true)
	}()

	att := &fakeAttach{socket: socket}
	require.NoError(t, attach(launch.ForTest(os.Getpid()), att, ew, socket))

	assert.True(t, att.upAtResolve, "the resolver was written before the sandbox's filesystem was in place")
}

// A workload whose resolver cannot be pointed at the gateway would come up
// resolving nothing, so the launch stops and nothing is registered.
func TestAttachStopsWhenTheResolverCannotBeWritten(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	marker := filepath.Join(t.TempDir(), "ran")

	l, socket := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker})

	att := &fakeAttach{resolveErr: errors.New("no /etc")}

	require.ErrorContains(t, attach(l, att, ew, socket), "no /etc")
	assert.Equal(t, []string{"wire", "resolve"}, att.calls)

	assertNeverRan(t, marker)
}

// Two instances of a workload that is not single instance each have a
// supervisor of their own. A launch talks only to its own, so an earlier
// instance answering first can neither stand in for the new sandbox being
// up nor take the release meant for it.
func TestAttachOwnInstanceOnly(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	ew.Workload.SingleInstance = false

	earlier, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)
	later, err := workloadAgentDir(ew, onGateway(1, "10.111.0.3"))
	require.NoError(t, err)

	marker := filepath.Join(t.TempDir(), "ran")
	go func() {
		_ = sandbox.Supervise(filepath.Join(earlier, "agent.sock"),
			[]string{"/bin/sh", "-c", "touch " + marker}, true)
	}()
	require.Eventually(t, func() bool {
		return sandbox.Ready(filepath.Join(earlier, "agent.sock")) == nil
	}, 5*time.Second, 10*time.Millisecond)

	socket := filepath.Join(later, "agent.sock")
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = sandbox.Supervise(socket, []string{"/bin/sh", "-c", "sleep 5"}, true)
	}()

	att := &fakeAttach{socket: socket}
	require.NoError(t, attach(launch.ForTest(os.Getpid()), att, ew, socket))

	assert.True(t, att.upAtResolve, "the resolver was written before this instance's sandbox was up")
	assertNeverRan(t, marker)
}

// A single instance workload has one supervisor, at the path a second
// launch finds it on to hand its command over.
func TestAgentDirIsSharedBySingleInstanceLaunches(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})

	want, err := files.WorkloadAgentDir(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)

	for _, att := range []*gateway.Attach{nil, onGateway(1, "10.111.0.2")} {
		got, err := workloadAgentDir(ew, att)
		require.NoError(t, err)
		assert.Equal(t, want, got)
		assert.DirExists(t, got)
	}
}

// Every launch of a workload that is not single instance is a sandbox of
// its own, and a supervisor socket shared between them is one the next
// launch unlinks from under the last. The gateway and the address tell
// them apart, since a gateway hands an address out once.
func TestAgentDirIsPerInstanceOnTheGateway(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})
	ew.Workload.SingleInstance = false

	parent, err := files.WorkloadAgentDir(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)

	first, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)
	second, err := workloadAgentDir(ew, onGateway(1, "10.111.0.3"))
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(parent, "1", "10.111.0.2"), first)
	assert.Equal(t, filepath.Join(parent, "1", "10.111.0.3"), second)
	assert.DirExists(t, first)
	assert.DirExists(t, second)
}

// A gateway is named by its start time since boot, which another boot can
// repeat. A directory already at the path was then left by a sandbox of an
// earlier boot, and the reap that would have removed it never ran.
func TestAgentDirClearsWhatADeadInstanceLeft(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})
	ew.Workload.SingleInstance = false

	dir, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, files.AgentSocketName), nil, 0o600))

	again, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)

	assert.Equal(t, dir, again)
	assert.NoFileExists(t, filepath.Join(dir, files.AgentSocketName))
}

// Without a gateway or the single instance flag nothing is supervised.
func TestAgentDirIsNotNeededWithoutASupervisor(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})
	ew.Workload.SingleInstance = false

	dir, err := workloadAgentDir(ew, nil)

	require.NoError(t, err)
	assert.Empty(t, dir)
}

// A gateway that is started again hands its addresses out again. The
// sandbox of an earlier gateway may still be running on one of them, and
// its reap must not take the directory of the launch that now holds it.
func TestAgentDirOutlivesTheReapOfAnEarlierGateway(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})
	ew.Workload.SingleInstance = false

	earlier, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)
	later, err := workloadAgentDir(ew, onGateway(2, "10.111.0.2"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(later, files.AgentSocketName), nil, 0o600))

	cmd := execabs.Command("/bin/true")
	require.NoError(t, cmd.Start())

	reap(cmd, sandbox.State{}, filepath.Join(t.TempDir(), "state"), nil, ew.Name, earlier)

	assert.FileExists(t, filepath.Join(later, files.AgentSocketName))
}

// The sandboxes of a gateway that has gone lost their network with it, and
// a launch made from a terminal is never reaped, so what they left is
// cleared by the first launch on the gateway that replaced it.
func TestAgentDirClearsWhatEarlierGatewaysLeft(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})
	ew.Workload.SingleInstance = false

	earlier, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)
	_, err = workloadAgentDir(ew, onGateway(2, "10.111.0.2"))
	require.NoError(t, err)

	assert.NoDirExists(t, filepath.Dir(earlier))
}

func onGateway(gw uint64, addr string) *gateway.Attach {
	return &gateway.Attach{Addr: netip.MustParseAddr(addr), Gateway: gw}
}

// assertNeverRan gives the workload the time it would have needed to run had
// it been released, and then checks that it did not.
func assertNeverRan(t *testing.T, marker string) {
	t.Helper()

	assert.Never(t, func() bool {
		_, err := os.Stat(marker)

		return err == nil
	}, 200*time.Millisecond, 20*time.Millisecond)
}

// An instance's directory is its own and nothing else will use it again, so
// it goes with the sandbox rather than piling up under the workload's.
func TestReapRemovesTheInstanceAgentDir(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})
	ew.Workload.SingleInstance = false

	dir, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, files.AgentSocketName), nil, 0o600))

	cmd := execabs.Command("/bin/true")
	require.NoError(t, cmd.Start())

	reap(cmd, sandbox.State{}, filepath.Join(t.TempDir(), "state"), nil, ew.Name, dir)

	assert.NoDirExists(t, dir)
}

// A single instance workload's directory is where the next launch looks for
// a supervisor, so it stays.
func TestReapKeepsTheSharedAgentDir(t *testing.T) {
	ew := supervised(t, []string{"/bin/true"})

	dir, err := workloadAgentDir(ew, onGateway(1, "10.111.0.2"))
	require.NoError(t, err)

	cmd := execabs.Command("/bin/true")
	require.NoError(t, cmd.Start())

	reap(cmd, sandbox.State{}, filepath.Join(t.TempDir(), "state"), nil, ew.Name, "")

	assert.DirExists(t, dir)
}
