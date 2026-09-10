// A real gateway cannot be started here, for the reasons run_test.go gives,
// so what a status says about one that is genuinely up is not covered. What
// is covered is everything the report is built from: the config it reads,
// the records it reads them beside, a record left behind by a gateway that
// crashed, the addresses handed out of the subnet, and when the gateway is
// asked whether it is ready at all.
package gateway

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/session"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusWithoutAConfig(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	st := g.Inspect(newTestSession(t), nil, failingReady(t))

	assert.Contains(t, st.ConfigProblem, "no qubesome config was loaded")
	assert.Empty(t, st.Image)
	assert.Empty(t, st.Subnet)

	out := render(t, st)
	assert.Contains(t, out, "config     no qubesome config was loaded")
	assert.NotContains(t, out, "image      ")
	assert.NotContains(t, out, "subnet     ")
	assert.NotContains(t, out, "addresses  ")
}

// A config with no gateway block is a supported configuration and not a
// config that failed to load. Saying the first for the second is the bug
// doctor's session checks were fixed for.
func TestStatusWithoutAGatewayBlock(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	st := g.Inspect(newTestSession(t), &types.Config{}, failingReady(t))

	assert.Contains(t, st.ConfigProblem, "no gateway is configured")
	assert.NotContains(t, st.ConfigProblem, "no qubesome config was loaded")
}

func TestStatusReportsTheConfiguredGateway(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	st := g.Inspect(newTestSession(t), testConfig(unusableConfig()), failingReady(t))

	require.Empty(t, st.ConfigProblem)
	assert.Equal(t, "ghcr.io/qubesome/gateway:latest", st.Image)
	assert.Equal(t, testSubnet, st.Subnet)

	// A path in a qubesome config is rooted at the config tree, so the
	// leading separator names that tree and not the root of the disk.
	assert.Equal(t, "/config/root/gateway.yml", st.Policy)
	assert.Empty(t, st.PolicyProblem)

	out := render(t, st)
	assert.Contains(t, out, "image      ghcr.io/qubesome/gateway:latest")
	assert.Contains(t, out, "policy     /config/root/gateway.yml")
	assert.Contains(t, out, "subnet     "+testSubnet)
}

func TestStatusReportsAPolicyPathThatLeavesTheConfigTree(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	cfg := unusableConfig()
	cfg.Config = "../gateway.yml"

	st := g.Inspect(newTestSession(t), testConfig(cfg), failingReady(t))

	assert.Empty(t, st.Policy)
	assert.NotEmpty(t, st.PolicyProblem)
	assert.Contains(t, render(t, st), "policy     cannot be resolved:")
}

func TestStatusReportsNothingRunning(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	s := newTestSession(t)

	st := g.Inspect(s, testConfig(unusableConfig()), failingReady(t))

	assert.False(t, st.HolderRunning)
	assert.False(t, st.Running)

	out := render(t, st)
	assert.Contains(t, out, "holder     not running (no live record in "+s.StatePath+")")
	assert.Contains(t, out, "gateway    not running (no live record in "+g.StatePath+")")

	// Readiness only says something once there is a gateway to ask.
	assert.NotContains(t, out, "readiness")
}

// A record outlives the process it names, and the pid in it may since have
// been given to something else. Alive compares the recorded start time,
// which is what makes such a record read as no gateway at all.
func TestStatusReadsAStaleRecordAsNotRunning(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	state := fmt.Sprintf(`{"pid":%d,"startTime":1}`, os.Getpid())
	require.NoError(t, os.WriteFile(g.StatePath, []byte(state), 0o600))

	st := g.Inspect(newTestSession(t), testConfig(unusableConfig()), failingReady(t))

	assert.False(t, st.Running)
	assert.Zero(t, st.PID)
	assert.Empty(t, st.ReadyErr)
	assert.Contains(t, render(t, st), "gateway    not running")
}

func TestStatusReportsARunningHolderAndGateway(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	s := newTestSession(t)

	require.NoError(t, sandbox.WriteState(s.StatePath, os.Getpid()))
	require.NoError(t, sandbox.WriteState(g.StatePath, os.Getpid()))

	st := g.Inspect(s, testConfig(unusableConfig()), func() error { return nil })

	assert.True(t, st.HolderRunning)
	assert.Equal(t, os.Getpid(), st.HolderPID)
	assert.True(t, st.Running)
	assert.Equal(t, os.Getpid(), st.PID)

	out := render(t, st)
	assert.Contains(t, out, fmt.Sprintf("holder     running, pid %d", os.Getpid()))
	assert.Contains(t, out, fmt.Sprintf("gateway    running, pid %d", os.Getpid()))
	assert.Contains(t, out, "readiness  the resolver, proxy and ruleset are up")
}

// A gateway that does not answer is a status worth reporting rather than
// something to wait out, so the failure becomes a line like any other.
func TestStatusReportsAGatewayThatIsNotReady(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	require.NoError(t, sandbox.WriteState(g.StatePath, os.Getpid()))

	st := g.Inspect(newTestSession(t), testConfig(unusableConfig()),
		func() error { return errors.New("context deadline exceeded") })

	assert.Equal(t, "context deadline exceeded", st.ReadyErr)
	assert.Contains(t, render(t, st), "readiness  not ready: context deadline exceeded")
}

func TestStatusReportsTheAddressesHandedOut(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	require.NoError(t, sandbox.WriteState(g.StatePath, os.Getpid()))

	require.NoError(t, os.WriteFile(g.AllocPath,
		[]byte(fmt.Sprintf(`{"subnet":%q,"allocated":3}`, testSubnet)), 0o600))

	st := g.Inspect(newTestSession(t), testConfig(unusableConfig()), func() error { return nil })

	assert.Equal(t, "10.111.0.1", st.GatewayAddr)
	assert.Equal(t, uint64(3), st.Allocated)
	assert.Equal(t, "10.111.0.4", st.LastAddr)
	assert.Contains(t, render(t, st), "addresses  10.111.0.1 is the gateway's own, 3 handed out up to 10.111.0.4")
}

// The count belongs to the gateway that is running, and the next launch
// clears it, so it says nothing while there is no gateway.
func TestStatusReportsOnlyTheGatewayAddressWithNoGatewayRunning(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	require.NoError(t, os.WriteFile(g.AllocPath,
		[]byte(fmt.Sprintf(`{"subnet":%q,"allocated":3}`, testSubnet)), 0o600))

	st := g.Inspect(newTestSession(t), testConfig(unusableConfig()), failingReady(t))

	assert.Contains(t, render(t, st), "addresses  10.111.0.1 is the gateway's own\n")
}

// A subnet changed under a running gateway is one of the things a status is
// for, so it is reported rather than refused the way a launch refuses it.
func TestStatusReportsASubnetTheRecordDoesNotMatch(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	require.NoError(t, os.WriteFile(g.AllocPath, []byte(`{"subnet":"10.112.0.0/24","allocated":1}`), 0o600))

	st := g.Inspect(newTestSession(t), testConfig(unusableConfig()), failingReady(t))

	assert.Contains(t, st.AddrProblem, "hands addresses out of 10.112.0.0/24")
	assert.Contains(t, render(t, st), "addresses  the running gateway hands addresses out of 10.112.0.0/24")
}

func TestStatusReportsAnUnusableSubnet(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	cfg := unusableConfig()
	cfg.Subnet = "10.111.0.0"

	st := g.Inspect(newTestSession(t), testConfig(cfg), failingReady(t))

	assert.Empty(t, st.GatewayAddr)
	assert.Contains(t, st.AddrProblem, "invalid gateway subnet")
}

// newTestSession returns a session whose files are in the test's own
// directory, so nothing here reads the user's session.
func newTestSession(t *testing.T) session.Session {
	t.Helper()

	dir := t.TempDir()

	return session.Session{
		Dir:       dir,
		LockPath:  filepath.Join(dir, "lock"),
		StatePath: filepath.Join(dir, "holder.json"),
	}
}

// testRoot stands in for the directory a qubesome config was read from,
// which is what a policy path in it is resolved against.
const testRoot = "/config/root"

func testConfig(gw types.GatewayConfig) *types.Config {
	return &types.Config{RootDir: testRoot, Gateway: &gw}
}

// failingReady is the readiness probe for a test that expects no gateway to
// be asked.
func failingReady(t *testing.T) func() error {
	t.Helper()

	return func() error {
		t.Error("the gateway was asked whether it is ready with none running")
		return nil
	}
}

func render(t *testing.T, st Status) string {
	t.Helper()

	var b strings.Builder
	require.NoError(t, st.Write(&b))

	return b.String()
}
