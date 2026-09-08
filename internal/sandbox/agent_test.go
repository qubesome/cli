package sandbox

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serving starts a supervisor on a socket in a temporary directory and
// returns it with the socket path. It is the supervisor without the main
// command, which is everything but the lifetime.
func serving(t *testing.T) (*supervisor, string) {
	t.Helper()

	socket := filepath.Join(t.TempDir(), "agent.sock")

	ln, err := listen(socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	s := &supervisor{}
	go s.serve(ln)

	return s, socket
}

func TestSpawnRunsTheArgvItIsGiven(t *testing.T) {
	t.Parallel()

	s, socket := serving(t)
	out := filepath.Join(t.TempDir(), "out")

	require.NoError(t, Spawn(socket, []string{"/bin/sh", "-c", "printf %s \"$0 $1\" > " + out, "one", "two"}))
	s.spawned.Wait()

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "one two", string(data))
}

// The supervisor answers once the process is started, not once it is
// finished. A workload is a long lived application, and a launch that
// waited for it would never return.
func TestSpawnReturnsWithoutWaitingForTheProcess(t *testing.T) {
	t.Parallel()

	_, socket := serving(t)

	// The sibling lets go of the standard streams it inherited, or the
	// test binary's own output pipe stays open for as long as it sleeps.
	start := time.Now()
	require.NoError(t, Spawn(socket, []string{"/bin/sh", "-c", "exec >/dev/null 2>&1; sleep 5"}))
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestSpawnReportsACommandThatCannotStart(t *testing.T) {
	t.Parallel()

	_, socket := serving(t)

	err := Spawn(socket, []string{filepath.Join(t.TempDir(), "not-there")})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoSupervisor,
		"a supervisor that answers is running the workload, so the launch must not start a second sandbox")
}

func TestSpawnWithoutASupervisor(t *testing.T) {
	t.Parallel()

	err := Spawn(filepath.Join(t.TempDir(), "agent.sock"), []string{"/bin/sh"})
	assert.ErrorIs(t, err, ErrNoSupervisor)
}

// A socket file with nothing listening on it is what a sandbox that died
// leaves behind, and it has to read as no supervisor rather than as a
// refusal.
func TestSpawnOnAStaleSocketFile(t *testing.T) {
	t.Parallel()

	socket := filepath.Join(t.TempDir(), "agent.sock")

	ln, err := listen(socket)
	require.NoError(t, err)

	addr, ok := ln.Addr().(*net.UnixAddr)
	require.True(t, ok)
	unlinkOnClose(t, ln)
	require.NoError(t, ln.Close())

	_, err = os.Stat(addr.Name)
	require.NoError(t, err, "the socket file has to still be there for this to be the stale case")

	assert.ErrorIs(t, Spawn(socket, []string{"/bin/sh"}), ErrNoSupervisor)
}

func TestSpawnRejectsAnEmptyArgv(t *testing.T) {
	t.Parallel()

	_, socket := serving(t)

	require.Error(t, Spawn(socket, nil))
}

// bwrap's reaper sits at pid 1 of the namespace it unshares, and the
// supervisor is its first child rather than the reaper itself, so a
// sibling started here is the supervisor's own child. Nothing else will
// wait for it while the supervisor is alive.
func TestSpawnReapsTheSibling(t *testing.T) {
	t.Parallel()

	s, socket := serving(t)
	pidFile := filepath.Join(t.TempDir(), "pid")

	require.NoError(t, Spawn(socket, []string{"/bin/sh", "-c", "echo $$ > " + pidFile}))
	s.spawned.Wait()

	data, err := os.ReadFile(pidFile)
	require.NoError(t, err)

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
	assert.ErrorIs(t, err, os.ErrNotExist, "a sibling that is not waited for is left as a zombie")
}

func TestSuperviseEndsWithTheMainCommand(t *testing.T) {
	t.Parallel()

	socket := filepath.Join(t.TempDir(), "agent.sock")
	done := make(chan error, 1)

	go func() { done <- Supervise(socket, []string{"/bin/sh", "-c", "sleep 0.3"}) }()

	require.Eventually(t, func() bool {
		return Spawn(socket, []string{"/bin/sh", "-c", "exit 0"}) == nil
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, <-done)

	assert.ErrorIs(t, Spawn(socket, []string{"/bin/sh"}), ErrNoSupervisor,
		"the sandbox is gone with the main command, so the socket must not answer")
}

func TestSuperviseReportsTheMainCommandExit(t *testing.T) {
	t.Parallel()

	socket := filepath.Join(t.TempDir(), "agent.sock")

	err := Supervise(socket, []string{"/bin/sh", "-c", "exit 3"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "3")
}

func TestSuperviseRejectsAnEmptyArgv(t *testing.T) {
	t.Parallel()

	require.Error(t, Supervise(filepath.Join(t.TempDir(), "agent.sock"), nil))
}

// The socket is what tells a second launch that this workload is already
// running, so a workload that starts without one can be started twice.
func TestSuperviseDoesNotRunWithoutASocket(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "ran")
	socket := filepath.Join(t.TempDir(), "missing", "agent.sock")

	require.Error(t, Supervise(socket, []string{"/bin/sh", "-c", "touch " + marker}))

	_, err := os.Stat(marker)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// A sandbox that is gone leaves its socket file behind, and the bind
// refuses a path that already exists.
func TestListenReplacesAStaleSocketFile(t *testing.T) {
	t.Parallel()

	socket := filepath.Join(t.TempDir(), "agent.sock")
	require.NoError(t, os.WriteFile(socket, nil, 0o600))

	ln, err := listen(socket)
	require.NoError(t, err)
	require.NoError(t, ln.Close())
}

// A unix socket address is a fixed size field, and a path over it is
// truncated rather than refused, which would leave the supervisor
// listening somewhere near where the host is looking.
func TestListenRejectsAPathThatDoesNotFit(t *testing.T) {
	t.Parallel()

	socket := filepath.Join(t.TempDir(), strings.Repeat("a", maxSocketPath))

	_, err := listen(socket)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "over the")
}

func TestArgvRoundTrip(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"one":              {"/bin/sh"},
		"flags":            {"/opt/chrome", "--user-data-dir=/data", "https://example.com"},
		"empty argument":   {"/bin/sh", "", "-c"},
		"spaces and nulls": {"/bin/sh", "-c", "echo a b\tc"},
		// argv is bytes rather than text. JSON would replace this with
		// U+FFFD and hand the workload a different file name.
		"invalid utf8": {"/bin/cat", "/home/user/M\xfcller.pdf"},
	}

	for name, argv := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeArgv(encodeArgv(argv))
			require.NoError(t, err)
			assert.Equal(t, argv, got)
		})
	}
}

func TestDecodeArgvRejects(t *testing.T) {
	t.Parallel()

	tests := map[string][]byte{
		"nothing":            {},
		"no terminator":      []byte("/bin/sh"),
		"an empty command":   {0},
		"an empty command 2": []byte("\x00-c\x00"),
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeArgv(payload)
			require.Error(t, err)
		})
	}
}

func TestFrameRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, writeFrame(&buf, []byte("payload")))
	got, err := readFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), got)
}

// An empty frame is how the supervisor says the process started.
func TestFrameRoundTripEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	require.NoError(t, writeFrame(&buf, nil))
	got, err := readFrame(&buf)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// The length is read before the payload, so a header naming more than any
// message can hold must be refused rather than allocated.
func TestReadFrameRejectsAnOversizeLength(t *testing.T) {
	t.Parallel()

	_, err := readFrame(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "over the")
}

func TestReadFrameRejectsATruncatedMessage(t *testing.T) {
	t.Parallel()

	_, err := readFrame(bytes.NewReader([]byte{0, 0, 0, 8, 'a', 'b'}))
	require.Error(t, err)
}

// unlinkOnClose keeps the socket file after the listener is closed, which
// is what a sandbox that died leaves behind. Go removes a socket it
// created on close unless it is told not to.
func unlinkOnClose(t *testing.T, ln net.Listener) {
	t.Helper()

	ul, ok := ln.(*net.UnixListener)
	require.True(t, ok)
	ul.SetUnlinkOnClose(false)
}
