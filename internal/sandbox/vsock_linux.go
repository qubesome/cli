package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// vsockBacklog is the accept queue for a guest listener. One caller
	// makes one call per launch, so the queue only ever holds a launch that
	// arrived while another was being served.
	vsockBacklog = 8

	// maxConnectLine bounds firecracker's answer to a connection request.
	// The answer is "OK " and a port number, and the limit is what stops a
	// peer that never sends a newline from being read until the deadline.
	maxConnectLine = 64
)

// SuperviseVM runs argv inside a VM, spawns siblings into it and opens
// consoles on it on request, until argv exits.
//
// It is Supervise reached over vsock rather than over a unix socket,
// because a guest has no filesystem in common with the host to put a
// socket file on. It also waits for what it starts differently, because
// it is pid 1 of the machine. See guestReaper.
//
// An empty argv is a machine with no command of its own, which Supervise
// refuses and this accepts. See consoleGate for what such a machine is
// and for what decides when it comes down.
func SuperviseVM(port, consolePort uint32, argv []string) error {
	// Both listeners come before the command for the same reason one does
	// in Supervise. A workload started before anything can reach it is a
	// workload that can be started twice.
	ln, err := listenVSOCK(port)
	if err != nil {
		return err
	}

	cln, err := listenVSOCK(consolePort)
	if err != nil {
		_ = ln.Close()

		return err
	}

	// The reaper is collecting before the first process is started, or
	// something could exit into a loop that is not running yet.
	reaper := startReaping()

	gate := newConsoleGate(consoleGrace)

	// Consoles are served on a port and an accept loop of their own. See
	// VMConsolePort for why they are not requests on the supervisor's.
	go serveConsoles(cln, reaper, gate)

	if len(argv) == 0 {
		return superviseConsoles(ln, reaper, gate)
	}

	return superviseWith(ln, argv, reaper)
}

// SpawnVM asks the supervisor listening on a guest port to start argv
// inside its VM.
//
// uds is firecracker's host side vsock socket, not an AF_VSOCK address.
// See dialVM.
func SpawnVM(uds string, port uint32, argv []string) error {
	if len(argv) == 0 {
		return errors.New("sandbox: spawn needs a command to run")
	}

	conn, err := dialVM(uds, port)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}

	return spawn(conn, argv)
}

// dialVM opens a connection to a guest listener through firecracker.
//
// The host end of a firecracker vsock device is a unix socket and the host
// never opens an AF_VSOCK socket of its own. Firecracker multiplexes every
// guest port over that one file, so a caller names the port it wants in a
// preamble before anything else crosses. That is also what keeps a VM's
// supervisor away from other workloads: reaching it is a question of who
// can open the file.
func dialVM(uds string, port uint32) (net.Conn, error) {
	d := net.Dialer{Timeout: exchangeTimeout}

	conn, err := d.DialContext(context.Background(), "unix", uds)
	if err != nil {
		return nil, err
	}

	// The handshake is bounded by the same deadline as the exchange that
	// follows it, which spawn resets once the connection is its own.
	if err := conn.SetDeadline(time.Now().Add(exchangeTimeout)); err != nil {
		_ = conn.Close()

		return nil, err
	}

	if err := vsockConnect(conn, port); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return conn, nil
}

// vsockConnect performs firecracker's host side handshake.
//
// Firecracker answers "OK <n>" once the guest has accepted, where n is the
// host side port it assigned and which nothing here needs. Any other
// answer is a refusal, and the usual cause is that nothing in the guest is
// listening on the port that was asked for.
func vsockConnect(conn net.Conn, port uint32) error {
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		return fmt.Errorf("sandbox: failed to ask for vsock port %d: %w", port, err)
	}

	line, err := readLine(conn)
	if err != nil {
		return fmt.Errorf("sandbox: failed to read the answer for vsock port %d: %w", port, err)
	}

	n, ok := strings.CutPrefix(line, "OK ")
	if !ok {
		return fmt.Errorf("sandbox: vsock port %d was refused: %q", port, line)
	}

	if _, err := strconv.ParseUint(n, 10, 32); err != nil {
		return fmt.Errorf("sandbox: vsock port %d answered %q, which names no port", port, line)
	}

	return nil
}

// readLine reads up to a newline and returns what came before it.
//
// It reads a byte at a time rather than through a buffered reader, which
// would be faster and wrong: a buffer takes bytes past the newline out of
// the connection, and those bytes are the first frame of the exchange the
// handshake exists to set up.
func readLine(r io.Reader) (string, error) {
	line := make([]byte, 0, maxConnectLine)

	var b [1]byte

	for len(line) < maxConnectLine {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return "", err
		}

		if b[0] == '\n' {
			return string(line), nil
		}

		line = append(line, b[0])
	}

	return "", fmt.Errorf("sandbox: a line of over %d bytes with no end to it", maxConnectLine)
}

// listenVSOCK binds a guest listener on port.
//
// It is hand-rolled because the standard library has no way to reach
// AF_VSOCK. net.Listen has no network name for it, and net.FileListener
// cannot adopt the descriptor either: newFileFD in net/file_posix.go
// switches on what Getsockname returns and answers EPROTONOSUPPORT for
// anything that is not IP or unix. What is left is little enough that a
// dependency for it would cost more than it saves.
func listenVSOCK(port uint32) (net.Listener, error) {
	// SOCK_NONBLOCK is what makes os.NewFile hand the descriptor to the
	// runtime poller below. Without it every wait would be a spin.
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to create a vsock socket: %w", err)
	}

	// VMADDR_CID_ANY takes connections from any context id, which in a
	// firecracker guest is the host and nothing else. A guest has no
	// peers.
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		_ = unix.Close(fd)

		return nil, fmt.Errorf("sandbox: failed to bind vsock port %d: %w", port, err)
	}

	if err := unix.Listen(fd, vsockBacklog); err != nil {
		_ = unix.Close(fd)

		return nil, fmt.Errorf("sandbox: failed to listen on vsock port %d: %w", port, err)
	}

	f := os.NewFile(uintptr(fd), "vsock")

	rc, err := f.SyscallConn()
	if err != nil {
		_ = f.Close()

		return nil, fmt.Errorf("sandbox: failed to take the vsock socket to the poller: %w", err)
	}

	return &vsockListener{f: f, rc: rc, addr: vsockAddr(fmt.Sprintf("vsock:%d", port))}, nil
}

// vsockListener is a net.Listener over an AF_VSOCK descriptor the runtime
// poller owns.
type vsockListener struct {
	f    *os.File
	rc   syscall.RawConn
	addr vsockAddr
}

func (l *vsockListener) Accept() (net.Conn, error) {
	var (
		nfd   int
		cause error
	)

	// RawConn.Read runs the function, and when it returns false waits on
	// the poller for the descriptor to become readable before running it
	// again. That is the whole reason for the poller here: a supervisor
	// waiting for a launch that may never come parks, where a loop around
	// a non-blocking accept would burn a core of a guest that has one.
	err := l.rc.Read(func(fd uintptr) bool {
		nfd, _, cause = unix.Accept4(int(fd), unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC)

		return !retryAccept(cause)
	})
	if err != nil {
		return nil, err
	}

	if cause != nil {
		return nil, fmt.Errorf("sandbox: failed to accept on %s: %w", l.addr, cause)
	}

	return &vsockConn{File: os.NewFile(uintptr(nfd), "vsock"), addr: l.addr}, nil
}

func (l *vsockListener) Close() error { return l.f.Close() }

func (l *vsockListener) Addr() net.Addr { return l.addr }

// retryAccept reports the failures that belong to one connection rather
// than to the listener. EAGAIN is the empty queue and is how the accept
// above comes to wait at all. A connection the peer dropped between the
// handshake and the accept must not end a supervisor's accept loop.
func retryAccept(err error) bool {
	return errors.Is(err, unix.EAGAIN) ||
		errors.Is(err, unix.EINTR) ||
		errors.Is(err, unix.ECONNABORTED)
}

// vsockConn is a net.Conn over an AF_VSOCK descriptor. The deadlines the
// protocol layer sets work because the descriptor is pollable.
type vsockConn struct {
	*os.File
	addr vsockAddr
}

func (c *vsockConn) LocalAddr() net.Addr { return c.addr }

func (c *vsockConn) RemoteAddr() net.Addr { return c.addr }

// vsockAddr names a vsock endpoint for the interfaces that insist on one.
// Nothing in the protocol layer reads an address.
type vsockAddr string

func (a vsockAddr) Network() string { return "vsock" }

func (a vsockAddr) String() string { return string(a) }

var (
	_ net.Listener = (*vsockListener)(nil)
	_ net.Conn     = (*vsockConn)(nil)
)
