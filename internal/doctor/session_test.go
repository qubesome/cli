package doctor

import (
	"errors"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gatewayConfig() *types.Config {
	return &types.Config{
		Gateway: &types.GatewayConfig{
			Image:  "ghcr.io/qubesome/gateway:latest",
			Config: "/gateway.yml",
			Subnet: "10.111.0.0/24",
		},
	}
}

func runningSession() *fakeEnv {
	return &fakeEnv{
		alive: map[string]bool{
			files.SessionStatePath(): true,
			files.GatewayStatePath(): true,
		},
	}
}

// A config with no gateway block is a supported configuration and not a
// missing one. Reporting it as a failure would tell every user who wants
// no egress that their installation is broken.
func TestSessionWithoutAGatewayBlock(t *testing.T) {
	t.Parallel()

	checks := Session(&fakeEnv{}, &types.Config{})

	require.Len(t, checks, 1)
	assert.Equal(t, OK, checks[0].Status)
	assert.Contains(t, checks[0].Detail, "no gateway is configured")
}

// A config that could not be read is not a config with no gateway in it.
// A bare qubesome doctor finds one only through a running profile, and it
// reported "no gateway is configured" on a host whose config configures
// one, which is a diagnostic saying something untrue.
func TestSessionWithNoConfigDoesNotClaimThereIsNoGateway(t *testing.T) {
	t.Parallel()

	checks := Session(&fakeEnv{}, nil)

	require.Len(t, checks, 1)
	assert.Equal(t, Warn, checks[0].Status)
	assert.Contains(t, checks[0].Detail, "no qubesome config was loaded")
	assert.NotContains(t, checks[0].Detail, "no gateway is configured")
}

// The holder is not asked about at all without a gateway block, because
// nothing starts one. A configuration with no gateway would otherwise be
// told its session is broken for want of a namespace it never needed.
func TestSessionWithoutAGatewayBlockIgnoresADeadHolder(t *testing.T) {
	t.Parallel()

	checks := Session(&fakeEnv{}, &types.Config{})

	for _, c := range checks {
		assert.NotEqual(t, "session holder", c.Name)
	}
}

func TestSessionRunning(t *testing.T) {
	t.Parallel()

	checks := Session(runningSession(), gatewayConfig())

	assert.Equal(t, OK, checkByName(t, checks, "session holder").Status)
	assert.Equal(t, OK, checkByName(t, checks, "session gateway").Status)
	assert.Equal(t, OK, checkByName(t, checks, "gateway readiness").Status)
}

// A configured gateway that is not running is the case a workload cannot
// be started in, so unlike an absent block it is a failure.
func TestSessionGatewayConfiguredButNotRunning(t *testing.T) {
	t.Parallel()

	env := runningSession()
	env.alive[files.GatewayStatePath()] = false

	checks := Session(env, gatewayConfig())

	gw := checkByName(t, checks, "session gateway")
	assert.Equal(t, Fail, gw.Status)
	assert.Contains(t, gw.Detail, files.GatewayStatePath())
	assert.NotEmpty(t, gw.Fix)
}

// Readiness is only worth asking about once there is a gateway to ask, and
// a second failure with the same cause would bury the first.
func TestSessionSkipsReadinessWithNoGateway(t *testing.T) {
	t.Parallel()

	env := runningSession()
	env.alive[files.GatewayStatePath()] = false
	env.gatewayReady = errors.New("dial unix: no such file or directory")

	for _, c := range Session(env, gatewayConfig()) {
		assert.NotEqual(t, "gateway readiness", c.Name)
	}
}

// A holder that is not running is warned about and not failed. It comes
// up with the first workload that needs a gateway address, so a host that
// has not opened one is idle rather than broken, which is how the profile
// sandbox check reads the same state.
func TestSessionHolderNotRunning(t *testing.T) {
	t.Parallel()

	env := runningSession()
	env.alive[files.SessionStatePath()] = false

	holder := checkByName(t, Session(env, gatewayConfig()), "session holder")
	assert.Equal(t, Warn, holder.Status)
	assert.Contains(t, holder.Detail, files.SessionStatePath())
}

// Neither running is an idle session. A holder without a gateway is not:
// something opened the namespace and then did not finish, and every
// workload on a gateway network fails closed until it does.
func TestSessionIdleIsNotAFailureButAHalfStartedOneIs(t *testing.T) {
	t.Parallel()

	idle := runningSession()
	idle.alive[files.SessionStatePath()] = false
	idle.alive[files.GatewayStatePath()] = false

	assert.Equal(t, Warn, checkByName(t, Session(idle, gatewayConfig()), "session gateway").Status)

	half := runningSession()
	half.alive[files.GatewayStatePath()] = false

	assert.Equal(t, Fail, checkByName(t, Session(half, gatewayConfig()), "session gateway").Status)
}

// A gateway whose sandbox is up but whose ruleset is not is the state a
// workload must never be started into, so the running sandbox alone is not
// taken as an answer.
func TestSessionGatewayRunningButNotReady(t *testing.T) {
	t.Parallel()

	env := runningSession()
	env.gatewayReady = errors.New("failed to wait for the gateway to be ready: context deadline exceeded")

	checks := Session(env, gatewayConfig())

	assert.Equal(t, OK, checkByName(t, checks, "session gateway").Status)

	ready := checkByName(t, checks, "gateway readiness")
	assert.Equal(t, Fail, ready.Status)
	assert.Contains(t, ready.Detail, "context deadline exceeded")
}

// The session is one per user rather than one per profile, so it is
// reported by a bare qubesome doctor and not only when a profile is named.
func TestRunReportsTheSessionWithoutAProfile(t *testing.T) {
	t.Parallel()

	report := Run(runningSession(), Options{Config: gatewayConfig()})

	checks := sectionByPrefix(t, report, "Session")
	assert.Equal(t, OK, checkByName(t, checks, "session gateway").Status)
}
