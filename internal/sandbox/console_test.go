package sandbox

import (
	"bytes"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestConsoleFrameCarriesItsKind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		kind consoleKind
		body []byte
	}{
		{"data", consoleData, []byte("some output")},
		{"window size", consoleWinSize, encodeWinSize(winSize{Rows: 24, Cols: 80})},
		{"exit status", consoleExit, encodeExitStatus(7)},
		{"an empty body", consoleData, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kind, body, err := splitConsoleFrame(consoleFrame(tc.kind, tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.kind, kind)
			assert.Equal(t, tc.body, nonEmpty(body))
		})
	}
}

// nonEmpty reads an empty body back as nil, so a frame built from nothing
// round trips to what it was built from.
func nonEmpty(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}

	return b
}

func TestConsoleFrameWithoutAKind(t *testing.T) {
	t.Parallel()

	_, _, err := splitConsoleFrame(nil)
	require.Error(t, err)
}

// The kinds are on the wire, so their numbers are part of the protocol
// and cannot be reordered without both ends changing together.
func TestConsoleKindNumbers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, consoleKind(0), consoleData)
	assert.Equal(t, consoleKind(1), consoleWinSize)
	assert.Equal(t, consoleKind(2), consoleExit)
}

func TestWindowSizeEncoding(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []byte{0, 24, 0, 80}, encodeWinSize(winSize{Rows: 24, Cols: 80}))
	assert.Equal(t, []byte{1, 0, 0, 255}, encodeWinSize(winSize{Rows: 256, Cols: 255}))

	ws, err := decodeWinSize([]byte{1, 0, 0, 255})
	require.NoError(t, err)
	assert.Equal(t, winSize{Rows: 256, Cols: 255}, ws)
}

func TestWindowSizeOfTheWrongLength(t *testing.T) {
	t.Parallel()

	for _, body := range [][]byte{nil, {0}, {0, 24, 0}, {0, 24, 0, 80, 0}} {
		_, err := decodeWinSize(body)
		require.Error(t, err)
	}
}

func TestExitStatusRoundTrip(t *testing.T) {
	t.Parallel()

	for _, status := range []int{0, 1, 7, 130, 255} {
		got, err := decodeExitStatus(encodeExitStatus(status))
		require.NoError(t, err)
		assert.Equal(t, status, got)
	}
}

// A status outside a byte is a bug somewhere above, and clamping is what
// stops the truncation of one turning a failure into a success.
func TestExitStatusOutsideAByte(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []byte{1}, encodeExitStatus(-1))
	assert.Equal(t, []byte{math.MaxUint8}, encodeExitStatus(256))
	assert.Equal(t, []byte{math.MaxUint8}, encodeExitStatus(math.MaxUint8+1))
}

func TestExitStatusOfTheWrongLength(t *testing.T) {
	t.Parallel()

	for _, body := range [][]byte{nil, {0, 0}} {
		_, err := decodeExitStatus(body)
		require.Error(t, err)
	}
}

func TestExitStatusOfAWait(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, exitStatus(syscall.WaitStatus(0)))
	assert.Equal(t, 7, exitStatus(syscall.WaitStatus(7<<8)))
	assert.Equal(t, 128+int(unix.SIGKILL), exitStatus(syscall.WaitStatus(unix.SIGKILL)))
}

func TestConsoleWritesTheGuestsOutput(t *testing.T) {
	t.Parallel()

	var wire bytes.Buffer
	w := &consoleWriter{w: &wire}
	require.NoError(t, w.send(consoleData, []byte("one ")))
	require.NoError(t, w.send(consoleData, []byte("two")))
	require.NoError(t, w.send(consoleExit, encodeExitStatus(3)))

	var out bytes.Buffer

	status, err := readConsole(&wire, &out)
	require.NoError(t, err)
	assert.Equal(t, 3, status)
	assert.Equal(t, "one two", out.String())
}

func TestConsoleRefusesAFrameKindItDoesNotKnow(t *testing.T) {
	t.Parallel()

	var wire bytes.Buffer
	require.NoError(t, writeFrame(&wire, consoleFrame(consoleKind(9), nil)))

	_, err := readConsole(&wire, io.Discard)
	require.Error(t, err)
}

// A connection that ends without a status is a machine that went away
// under the session, and it must not read as a command that succeeded.
func TestConsoleEndingWithoutAnExitStatus(t *testing.T) {
	t.Parallel()

	var wire bytes.Buffer
	w := &consoleWriter{w: &wire}
	require.NoError(t, w.send(consoleData, []byte("half a line")))

	_, err := readConsole(&wire, io.Discard)
	require.Error(t, err)
}

// requirePTY skips a test that needs to allocate a pseudo terminal where
// there is none.
func requirePTY(t *testing.T) {
	t.Helper()

	master, slave, err := openPTY()
	if err != nil {
		t.Skipf("no pty can be allocated here: %v", err)
	}

	_ = slave.Close()
	_ = master.Close()
}

// consoleInput returns the read end of a pipe standing in for a
// terminal's input, with what should be typed into it already written.
func consoleStdin(t *testing.T, typed string) *os.File {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	go func() {
		defer w.Close()

		_, _ = io.WriteString(w, typed)
	}()

	return r
}

// A full session, both halves in this process over net.Pipe. There is no
// VM, no vsock and no firecracker anywhere in it, which is the point: the
// pty and the framing are the parts that can be wrong.
func TestConsoleSessionRunsACommandAndReportsHowItEnded(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	host, guest := net.Pipe()
	t.Cleanup(func() { _ = host.Close() })

	go serveConsole(guest, procStarter{})

	require.NoError(t, exchange(host, []string{"/bin/sh", "-c", "echo hello; exit 7"}))

	var out bytes.Buffer

	status, err := console(host, consoleStdin(t, ""), &out)
	require.NoError(t, err)
	assert.Equal(t, 7, status)
	assert.Contains(t, out.String(), "hello")
}

func TestConsoleSessionCarriesWhatIsTyped(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	host, guest := net.Pipe()
	t.Cleanup(func() { _ = host.Close() })

	go serveConsole(guest, procStarter{})

	require.NoError(t, exchange(host, []string{"/bin/cat"}))

	var out bytes.Buffer

	// The end of transmission character is what ends a read on a pty in
	// canonical mode, which is the mode the slave is in until something
	// in the guest changes it. Closing this end of the pipe would only
	// end the forwarding, and cat would keep waiting.
	status, err := console(host, consoleStdin(t, "typed\n\x04"), &out)
	require.NoError(t, err)
	assert.Equal(t, 0, status)
	assert.Contains(t, out.String(), "typed")
}

func TestConsoleReportsACommandThatCannotStart(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	host, guest := net.Pipe()
	t.Cleanup(func() { _ = host.Close() })

	go serveConsole(guest, procStarter{})

	err := exchange(host, []string{filepath.Join(t.TempDir(), "not-there")})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoSupervisor,
		"a supervisor that answers is there, so the failure is the command's and not the machine's")
}

func TestConsoleRefusesAnEmptyArgv(t *testing.T) {
	t.Parallel()

	_, err := ConsoleVM(filepath.Join(t.TempDir(), "vsock.sock"), VMConsolePort, nil)
	require.Error(t, err)
}

func TestConsoleWithoutASupervisor(t *testing.T) {
	t.Parallel()

	_, err := ConsoleVM(filepath.Join(t.TempDir(), "vsock.sock"), VMConsolePort, []string{"/bin/sh"})
	assert.ErrorIs(t, err, ErrNoSupervisor)
}

// ptyWinSize reads a pty's size without taking the master out of the
// runtime poller, which Fd would. See resizePTY.
func ptyWinSize(t *testing.T, ptmx *os.File) winSize {
	t.Helper()

	rc, err := ptmx.SyscallConn()
	require.NoError(t, err)

	var ws *unix.Winsize

	require.NoError(t, rc.Control(func(fd uintptr) {
		ws, err = unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ)
	}))
	require.NoError(t, err)

	return winSize{Rows: ws.Row, Cols: ws.Col}
}

func TestConsoleResizesTheGuestTerminal(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	ptmx, tty, err := openPTY()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tty.Close()
		_ = ptmx.Close()
	})

	host, guest := net.Pipe()
	t.Cleanup(func() { _ = host.Close() })

	go func() { _ = consoleInput(guest, ptmx) }()

	want := winSize{Rows: 24, Cols: 100}
	require.NoError(t, (&consoleWriter{w: host}).send(consoleWinSize, encodeWinSize(want)))

	assert.Eventually(t, func() bool {
		return ptyWinSize(t, ptmx) == want
	}, time.Second, 10*time.Millisecond)
}

func TestConsoleRefusesAWindowSizeItCannotRead(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	ptmx, tty, err := openPTY()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tty.Close()
		_ = ptmx.Close()
	})

	host, guest := net.Pipe()
	t.Cleanup(func() { _ = host.Close() })

	done := make(chan error, 1)
	go func() { done <- consoleInput(guest, ptmx) }()

	require.NoError(t, (&consoleWriter{w: host}).send(consoleWinSize, []byte{0, 24}))
	require.Error(t, <-done)
}
