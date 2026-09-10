package sandbox

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recordOf(t *testing.T, pid int) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, WriteState(path, pid))

	return path
}

func TestExited(t *testing.T) {
	t.Parallel()

	t.Run("a running process has not", func(t *testing.T) {
		t.Parallel()

		cmd := exec.CommandContext(t.Context(), "sleep", "60")
		require.NoError(t, cmd.Start())
		t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

		assert.False(t, Exited(recordOf(t, cmd.Process.Pid)))
	})

	// A process killed by something that is not its parent is left for
	// whoever reaps it. Until then its /proc entry is there and its start
	// time still matches, so a liveness check alone reads it as running
	// when it is only waiting to be collected.
	t.Run("a zombie has", func(t *testing.T) {
		t.Parallel()

		cmd := exec.CommandContext(t.Context(), "sleep", "60")
		require.NoError(t, cmd.Start())
		pid := cmd.Process.Pid
		path := recordOf(t, pid)

		require.NoError(t, cmd.Process.Kill())
		require.Eventually(t, func() bool { return Exited(path) }, 5*time.Second, 10*time.Millisecond,
			"a killed process nothing has reaped must read as exited")

		// Still unreaped, so Alive is what it was: the two disagree, and
		// that disagreement is the whole point.
		assert.True(t, Alive(path), "the record still names a process /proc knows about")

		_, _ = cmd.Process.Wait()
	})

	t.Run("a process that is gone has", func(t *testing.T) {
		t.Parallel()

		cmd := exec.CommandContext(t.Context(), "true")
		require.NoError(t, cmd.Start())
		pid := cmd.Process.Pid
		path := recordOf(t, pid)
		_ = cmd.Wait()

		assert.True(t, Exited(path))
	})

	t.Run("no record at all has", func(t *testing.T) {
		t.Parallel()

		assert.True(t, Exited(filepath.Join(t.TempDir(), "absent.json")))
	})
}
