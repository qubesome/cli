package bwrap

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/runners/util/launch"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
func gatedLaunch(t *testing.T, ew types.EffectiveWorkload, argv []string) *launch.Launcher {
	t.Helper()

	dir, err := files.WorkloadAgentDir(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, files.DirMode))

	socket, err := files.WorkloadAgentSocket(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)

	go func() { _ = sandbox.Supervise(socket, argv, true) }()

	return launch.ForTest(os.Getpid())
}

// The whole ordering. The sandbox exists, the veth is built and addressed,
// the gateway is told whose address it is, and only then is the workload
// released into a namespace that is already wired.
func TestAttachWiresAndRegistersBeforeReleasing(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	marker := filepath.Join(t.TempDir(), "ran")

	l := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker + "; sleep 5"})

	att := &fakeAttach{}
	require.NoError(t, attach(l, att, ew))

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

	l := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker})

	att := &fakeAttach{wireErr: errors.New("no veth")}

	require.ErrorContains(t, attach(l, att, ew), "no veth")
	assert.Equal(t, []string{"wire"}, att.calls)

	assertNeverRan(t, marker)
}

// A gateway that refuses the workload's name has no policy for it, and a
// workload with no policy must not reach the network at all.
func TestAttachStopsWhenRegisterFails(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	marker := filepath.Join(t.TempDir(), "ran")

	l := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker})

	att := &fakeAttach{registerErr: errors.New("unknown workload")}

	require.ErrorContains(t, attach(l, att, ew), "unknown workload")
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
	require.NoError(t, attach(launch.ForTest(os.Getpid()), att, ew))

	assert.True(t, att.upAtResolve, "the resolver was written before the sandbox's filesystem was in place")
}

// A workload whose resolver cannot be pointed at the gateway would come up
// resolving nothing, so the launch stops and nothing is registered.
func TestAttachStopsWhenTheResolverCannotBeWritten(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh", "-c", "sleep 5"})
	marker := filepath.Join(t.TempDir(), "ran")

	l := gatedLaunch(t, ew, []string{"/bin/sh", "-c", "touch " + marker})

	att := &fakeAttach{resolveErr: errors.New("no /etc")}

	require.ErrorContains(t, attach(l, att, ew), "no /etc")
	assert.Equal(t, []string{"wire", "resolve"}, att.calls)

	assertNeverRan(t, marker)
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
