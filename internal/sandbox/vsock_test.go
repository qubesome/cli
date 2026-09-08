package sandbox

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firecrackerStub listens on a unix socket the way firecracker's host side
// vsock device does. It reads the connection request and hands the
// connection, and the request line it read, to answer.
func firecrackerStub(t *testing.T, answer func(conn net.Conn, request string)) string {
	t.Helper()

	socket := filepath.Join(t.TempDir(), "vsock.sock")

	lc := net.ListenConfig{}

	ln, err := lc.Listen(t.Context(), "unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			request, err := readLine(conn)
			if err != nil {
				_ = conn.Close()

				continue
			}

			answer(conn, request)
		}
	}()

	return socket
}

func TestSpawnVMAsksForTheGuestPort(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	s := &supervisor{starter: procStarter{}}

	// Once the handshake is done the connection is an ordinary supervisor
	// exchange, so the stub hands it to the supervisor unchanged. That is
	// the point of the transport being a separate piece.
	socket := firecrackerStub(t, func(conn net.Conn, request string) {
		requests <- request
		_, _ = io.WriteString(conn, "OK 3\n")
		s.handle(conn)
	})

	out := filepath.Join(t.TempDir(), "out")

	require.NoError(t, SpawnVM(socket, 1234, []string{"/bin/sh", "-c", "printf %s \"$0\" > " + out, "ran"}))
	s.spawned.Wait()

	assert.Equal(t, "CONNECT 1234", <-requests)

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "ran", string(data))
}

func TestSpawnVMOnAnAnswerThatIsNotAConnection(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		answer string
		wantIn string
	}{
		"a refusal":              {answer: "ERR no listener\n", wantIn: `"ERR no listener"`},
		"a port that is no port": {answer: "OK not-a-port\n", wantIn: "names no port"},
		"an empty line":          {answer: "\n", wantIn: `""`},
		"nothing at all":         {answer: "", wantIn: "failed to read the answer"},
		"a line with no end":     {answer: strings.Repeat("O", 4096), wantIn: "no end to it"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			socket := firecrackerStub(t, func(conn net.Conn, _ string) {
				_, _ = io.WriteString(conn, tc.answer)
				_ = conn.Close()
			})

			err := SpawnVM(socket, 1234, []string{"/bin/sh", "-c", "exit 0"})
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrNoSupervisor)
			assert.Contains(t, err.Error(), tc.wantIn)
		})
	}
}

// A refused connection carries no guest at the other end, so putting a
// request on it would leave the caller waiting for the deadline for an
// answer that cannot come.
func TestSpawnVMSendsNothingAfterARefusal(t *testing.T) {
	t.Parallel()

	rest := make(chan []byte, 1)

	socket := firecrackerStub(t, func(conn net.Conn, _ string) {
		defer conn.Close()

		_, _ = io.WriteString(conn, "ERR no listener\n")

		b, _ := io.ReadAll(conn)
		rest <- b
	})

	require.Error(t, SpawnVM(socket, 1234, []string{"/bin/sh", "-c", "exit 0"}))
	assert.Empty(t, <-rest)
}

// The host reaches a guest through a file, so a VM that is gone reads the
// same way a sandbox that is gone does.
func TestSpawnVMWithoutAVM(t *testing.T) {
	t.Parallel()

	err := SpawnVM(filepath.Join(t.TempDir(), "vsock.sock"), 1234, []string{"/bin/sh"})
	assert.ErrorIs(t, err, ErrNoSupervisor)
}

func TestSpawnVMRejectsAnEmptyArgv(t *testing.T) {
	t.Parallel()

	socket := firecrackerStub(t, func(conn net.Conn, _ string) {
		_, _ = io.WriteString(conn, "OK 3\n")
		_ = conn.Close()
	})

	require.Error(t, SpawnVM(socket, 1234, nil))
}

func TestSuperviseVMRejectsAnEmptyArgv(t *testing.T) {
	t.Parallel()

	require.Error(t, SuperviseVM(1234, nil))
}

func TestReadLine(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   string
		want string
		err  bool
	}{
		"a line":                {in: "OK 3\n", want: "OK 3"},
		"only the first line":   {in: "OK 3\nmore\n", want: "OK 3"},
		"an empty line":         {in: "\n", want: ""},
		"nothing":               {in: "", err: true},
		"no newline":            {in: "OK 3", err: true},
		"a line at the limit":   {in: strings.Repeat("O", maxConnectLine-1) + "\n", want: strings.Repeat("O", maxConnectLine-1)},
		"a line over the limit": {in: strings.Repeat("O", maxConnectLine) + "\n", err: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := readLine(strings.NewReader(tc.in))
			if tc.err {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
