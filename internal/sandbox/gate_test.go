package sandbox

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A gated supervisor holds the workload until the host says its address is
// wired and registered, so the workload's first name lookup cannot precede
// the resolver it is meant to reach.
func TestAGatedSupervisorWaitsForItsRelease(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")
	marker := filepath.Join(dir, "ran")

	done := make(chan error, 1)
	go func() { done <- Supervise(socket, []string{"/bin/sh", "-c", "touch " + marker}, true) }()

	require.Eventually(t, func() bool {
		return answering(t, socket)
	}, 5*time.Second, 10*time.Millisecond)

	// The socket answers, so the supervisor is up. What must not have
	// happened yet is the workload running.
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist, "the workload ran before it had an address")

	require.NoError(t, Release(socket))
	require.NoError(t, <-done)

	assert.FileExists(t, marker)
}

// A gate that expires kills the sandbox rather than starting the workload
// unpoliced, which is the same fail-closed rule the gateway's own lifecycle
// follows. Returning here is what ends the sandbox.
func TestAnExpiredGateNeverStartsTheWorkload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")

	lc := net.ListenConfig{}

	ln, err := lc.Listen(t.Context(), "unix", filepath.Join(dir, "agent.sock"))
	require.NoError(t, err)

	g := &gate{released: make(chan struct{}), grace: 10 * time.Millisecond}

	err = superviseWith(ln, []string{"/bin/sh", "-c", "touch " + marker}, procStarter{}, g)

	require.ErrorContains(t, err, "gateway address")

	_, err = os.Stat(marker)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// A release arriving at a supervisor that was never gated is a host and a
// sandbox disagreeing about whether this workload has an address, which is
// worth reporting rather than ignoring.
func TestReleasingAnUngatedSupervisorIsRefused(t *testing.T) {
	t.Parallel()

	socket := filepath.Join(t.TempDir(), "agent.sock")

	go func() { _ = Supervise(socket, []string{"/bin/sh", "-c", "sleep 5"}, false) }()

	require.Eventually(t, func() bool {
		return answering(t, socket)
	}, 5*time.Second, 10*time.Millisecond)

	require.ErrorContains(t, Release(socket), "no gate")
}

// The gate is a message on the socket the supervisor already listens on, so
// a gated supervisor still spawns siblings while it is holding its own
// workload back.
func TestAGatedSupervisorStillAnswersSpawns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")
	sibling := filepath.Join(dir, "sibling")

	go func() { _ = Supervise(socket, []string{"/bin/sh", "-c", "sleep 5"}, true) }()

	require.Eventually(t, func() bool {
		return Spawn(socket, []string{"/bin/sh", "-c", "touch " + sibling}) == nil
	}, 5*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		_, err := os.Stat(sibling)

		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
}

// Nothing sends two releases, and a supervisor that panicked on one would
// take its sandbox down with it.
func TestASecondReleaseIsHarmless(t *testing.T) {
	t.Parallel()

	g := newGate(true)

	require.NoError(t, g.open())
	require.NoError(t, g.open())
	require.NoError(t, g.wait())
}

// answering reports whether a supervisor is listening, without asking it to
// do anything. A gated one is up long before it runs anything.
func answering(t *testing.T, socket string) bool {
	t.Helper()

	d := net.Dialer{}

	conn, err := d.DialContext(t.Context(), "unix", socket)
	if err != nil {
		return false
	}
	conn.Close()

	return true
}
