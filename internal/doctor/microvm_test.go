package doctor

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/gateway"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wiredEnv(w gateway.VMWiring) *fakeEnv {
	return &fakeEnv{wiring: w}
}

func healthy() gateway.VMWiring {
	return gateway.VMWiring{BridgeUp: true, VethEnslaved: true, TapEnslaved: true, Guarded: true}
}

func TestMicroVMChecksPassOnAWholeWire(t *testing.T) {
	t.Parallel()

	checks := microVMChecks(wiredEnv(healthy()), gatewayConfig().Gateway, 1234, "10.111.0.2")

	for _, name := range []string{"microvm sandbox", "microvm wiring", "microvm guard"} {
		assert.Equal(t, OK, checkByName(t, checks, name).Status, name)
	}
}

// The pid and the address are what an operator reaches for next, so they
// are in the report even when everything passes.
func TestMicroVMSandboxCheckNamesThePIDAndTheAddress(t *testing.T) {
	t.Parallel()

	c := checkByName(t, microVMChecks(wiredEnv(healthy()), gatewayConfig().Gateway, 1234, "10.111.0.2"),
		"microvm sandbox")

	assert.Contains(t, c.Detail, "1234")
	assert.Contains(t, c.Detail, "10.111.0.2")
}

// The failure that looks like success, and the reason this check exists.
func TestMicroVMChecksSayWhenTheTapIsNotABridgePort(t *testing.T) {
	t.Parallel()

	w := healthy()
	w.TapEnslaved = false

	c := checkByName(t, microVMChecks(wiredEnv(w), gatewayConfig().Gateway, 1234, "10.111.0.2"), "microvm wiring")

	assert.Equal(t, Fail, c.Status)
	assert.Contains(t, c.Detail, "tap0")
	assert.NotEmpty(t, c.Fix)
}

func TestMicroVMChecksSayWhenTheBridgeIsDown(t *testing.T) {
	t.Parallel()

	w := healthy()
	w.BridgeUp = false

	c := checkByName(t, microVMChecks(wiredEnv(w), gatewayConfig().Gateway, 1234, "10.111.0.2"), "microvm wiring")

	assert.Equal(t, Fail, c.Status)
	assert.Contains(t, c.Detail, "br0")
}

// A guest that tried to send under another address, and a guard that
// stopped it. The guard worked, so this is a warning rather than a
// failure.
func TestMicroVMChecksWarnOnASpoofCount(t *testing.T) {
	t.Parallel()

	w := healthy()
	w.Spoofed = 12

	c := checkByName(t, microVMChecks(wiredEnv(w), gatewayConfig().Gateway, 1234, "10.111.0.2"), "microvm guard")

	assert.Equal(t, Warn, c.Status)
	assert.Contains(t, c.Detail, "12")
}

// A machine running with no guard is the state the launch exists to make
// impossible, so seeing one is a failure rather than a warning.
func TestMicroVMChecksFailWithNoGuard(t *testing.T) {
	t.Parallel()

	w := healthy()
	w.Guarded = false

	c := checkByName(t, microVMChecks(wiredEnv(w), gatewayConfig().Gateway, 1234, "10.111.0.2"), "microvm guard")

	assert.Equal(t, Fail, c.Status)
}

// A namespace that cannot be read is one check saying so, not three
// repeating it.
func TestMicroVMChecksReportAnUnreadableNamespaceOnce(t *testing.T) {
	t.Parallel()

	env := &fakeEnv{wiringErr: errors.New("no gateway is running")}

	checks := microVMChecks(env, gatewayConfig().Gateway, 1234, "10.111.0.2")

	assert.Len(t, checks, 1)
	assert.Equal(t, Fail, checks[0].Status)
	assert.Contains(t, checks[0].Detail, "no gateway is running")
}

// A machine launched with no gateway recorded no address, and there is no
// wire to describe, so the checks are absent rather than failing.
func TestRunningMicroVMIgnoresAMachineWithNoAddress(t *testing.T) {
	t.Parallel()

	path := filepath.Join(files.ProfileDir("personal"), "sandbox-dev.json")
	env := &fakeEnv{
		alive:   map[string]bool{path: true},
		records: map[string]sandbox.State{path: {PID: 1234}},
	}

	ew := types.EffectiveWorkload{Workload: types.Workload{Runner: "firecracker"}}

	_, ok := runningMicroVM(env, gatewayConfig(), ew, "personal", "dev")
	assert.False(t, ok)
}

// A sandbox workload has no namespace of this shape to read.
func TestRunningMicroVMIgnoresASandboxWorkload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(files.ProfileDir("personal"), "sandbox-chrome.json")
	env := &fakeEnv{
		alive:   map[string]bool{path: true},
		records: map[string]sandbox.State{path: {PID: 1234, Address: "10.111.0.3"}},
	}

	_, ok := runningMicroVM(env, gatewayConfig(), types.EffectiveWorkload{}, "personal", "chrome")
	assert.False(t, ok)
}

// A state file outlives the process it names, so a machine that is not
// running has nothing to report either.
func TestRunningMicroVMIgnoresADeadRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(files.ProfileDir("personal"), "sandbox-dev.json")
	env := &fakeEnv{
		alive:   map[string]bool{path: false},
		records: map[string]sandbox.State{path: {PID: 1234, Address: "10.111.0.2"}},
	}

	ew := types.EffectiveWorkload{Workload: types.Workload{Runner: "firecracker"}}

	_, ok := runningMicroVM(env, gatewayConfig(), ew, "personal", "dev")
	assert.False(t, ok)
}

func TestRunningMicroVMFindsAMachineOnTheGateway(t *testing.T) {
	t.Parallel()

	path := filepath.Join(files.ProfileDir("personal"), "sandbox-dev.json")
	env := &fakeEnv{
		alive:   map[string]bool{path: true},
		records: map[string]sandbox.State{path: {PID: 1234, Address: "10.111.0.2"}},
	}

	ew := types.EffectiveWorkload{Workload: types.Workload{Runner: "firecracker"}}

	rec, ok := runningMicroVM(env, gatewayConfig(), ew, "personal", "dev")
	require.True(t, ok)
	assert.Equal(t, 1234, rec.PID)
	assert.Equal(t, "10.111.0.2", rec.Address)
}
