// Stopping a real gateway cannot be tested here, for the reasons
// run_test.go gives: no sandbox starts in the development container, so
// there is never one to take down. What is covered is what Stop decides:
// which pid it signals, that it signals it at all, and the two cases where
// there is nothing to signal.
package gateway

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/execabs"
)

// A session with no gateway in it is the state the caller asked for.
func TestStopWithNoGatewayRunning(t *testing.T) {
	t.Parallel()

	pid, err := newSessionGateway(t).Stop()

	require.NoError(t, err)
	assert.Zero(t, pid)
}

// A host that has never started a session has no session directory, and
// asking it to stop a gateway is still not an error.
func TestStopWithoutASessionDir(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)
	g.Dir = filepath.Join(g.Dir, "session")
	g.LockPath = filepath.Join(g.Dir, "gateway.lock")
	g.StatePath = filepath.Join(g.Dir, "sandbox-gateway.json")

	pid, err := g.Stop()

	require.NoError(t, err)
	assert.Zero(t, pid)
}

// A record left behind by a gateway that crashed names a pid the kernel may
// since have given to something else. It reads as no gateway, so nothing is
// signalled, and the record goes because reap would have removed it.
func TestStopRemovesARecordLeftBehind(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	state := fmt.Sprintf(`{"pid":%d,"startTime":1}`, os.Getpid())
	require.NoError(t, os.WriteFile(g.StatePath, []byte(state), 0o600))

	pid, err := g.Stop()

	require.NoError(t, err)
	assert.Zero(t, pid)
	assert.NoFileExists(t, g.StatePath)
}

// The pid in the record is the one that is signalled, and the record it came
// from does not outlive it.
func TestStopKillsTheRecordedProcess(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := execabs.CommandContext(ctx, "sleep", "60")
	require.NoError(t, cmd.Start())

	require.NoError(t, sandbox.WriteState(g.StatePath, cmd.Process.Pid))

	pid, err := g.Stop()

	require.NoError(t, err)
	assert.Equal(t, cmd.Process.Pid, pid)
	assert.NoFileExists(t, g.StatePath)

	err = cmd.Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "killed")
}

// Nothing is signalled twice. The second call finds a record naming a
// process that is gone, which is the crashed gateway case again.
func TestStopIsRepeatable(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := execabs.CommandContext(ctx, "sleep", "60")
	require.NoError(t, cmd.Start())

	require.NoError(t, sandbox.WriteState(g.StatePath, cmd.Process.Pid))

	_, err := g.Stop()
	require.NoError(t, err)
	_ = cmd.Wait()

	pid, err := g.Stop()
	require.NoError(t, err)
	assert.Zero(t, pid)
}

// The lock Stop takes is the one a launch takes to start a gateway, so it is
// released by the time Stop returns and a launch is not left waiting on it.
func TestStopReleasesTheGatewayLock(t *testing.T) {
	t.Parallel()

	g := newSessionGateway(t)

	_, err := g.Stop()
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)

		lock, err := acquire(g.LockPath)
		if err == nil {
			lock.Close()
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway lock was still held after Stop returned")
	}
}
