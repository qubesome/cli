package sandbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/execabs"
)

// SuperviseCommand is the qubesome subcommand that supervises a workload
// from inside its sandbox. It is named here because both ends need it:
// the workload spec puts it on the sandbox's command line and the cli
// registers a command under it.
const SuperviseCommand = "supervise"

// ErrNoSupervisor reports that nothing answered on a supervisor's socket.
//
// It separates the two failures a caller has to tell apart. A sandbox that
// cannot be reached is one to replace, and a supervisor that answers and
// refuses is one that is already running the workload.
var ErrNoSupervisor = errors.New("sandbox: no supervisor answered")

const (
	// maxFrame bounds one message. An argv is a few kilobytes at most, and
	// the limit is what stops a length header asking for an allocation the
	// size of the field it was read from.
	maxFrame = 1 << 20

	// exchangeTimeout bounds one request and its answer. Both ends are
	// local and the work between them is a fork and an exec, so anything
	// slower than this is a peer that has stopped rather than a slow one.
	exchangeTimeout = 10 * time.Second

	// maxSocketPath is the size of sun_path in a unix socket address on
	// Linux, the terminator included. A longer path is truncated by the
	// kernel rather than refused, which would leave a supervisor listening
	// somewhere near where the host is looking.
	maxSocketPath = 108
)

// Supervise runs argv inside the sandbox and spawns siblings into it on
// request, until argv exits.
//
// It exists because a sandbox cannot be re-entered. Joining its pid
// namespace needs privileges an ordinary user does not have, by any route,
// and the pid namespace is the part that matters: a single instance
// application decides whether it is already running by checking a pid, and
// from another pid namespace a live process is not there. So the second
// launch of a single instance workload is handed to a process already
// inside the sandbox rather than entering it.
func Supervise(socket string, argv []string) error {
	if len(argv) == 0 {
		return errors.New("sandbox: supervise needs a command to run")
	}

	// The socket comes before the command it belongs to. It is what tells
	// a second launch that this workload is already running, and a
	// workload started before it exists is a workload that can be started
	// twice. A socket that cannot be created fails the launch for the same
	// reason: one application that does not open is a message on a
	// terminal, and two of a single instance application sharing one
	// profile directory is a corrupted profile.
	ln, err := listen(socket)
	if err != nil {
		return err
	}
	defer ln.Close()

	main, err := start(argv)
	if err != nil {
		return err
	}

	s := &supervisor{}
	go s.serve(ln)

	// The sandbox's lifetime is the main command's, exactly as the
	// container's was. Returning ends this process, and this process is
	// the first child of the pid namespace bwrap unshared, so the kernel
	// tears that namespace down and every sibling spawned into it goes
	// with it. Siblings are deliberately neither waited for nor signalled
	// first: a sibling is another window of the same application, and the
	// container ended them the same way when its main process exited.
	return main.Wait()
}

// Spawn asks the supervisor listening on socket to start argv inside its
// sandbox.
//
// There is no authentication on the exchange and none is wanted. See
// listen.
func Spawn(socket string, argv []string) error {
	if len(argv) == 0 {
		return errors.New("sandbox: spawn needs a command to run")
	}

	d := net.Dialer{Timeout: exchangeTimeout}

	conn, err := d.DialContext(context.Background(), "unix", socket)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(exchangeTimeout)); err != nil {
		return fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}

	if err := writeFrame(conn, encodeArgv(argv)); err != nil {
		return fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}

	res, err := readFrame(conn)
	if err != nil {
		// The request went out and no answer came back, so whether
		// anything started is unknown. It reads as no supervisor because a
		// supervisor that drops a connection half way through is on its
		// way out, and its sandbox with it.
		return fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}

	if len(res) > 0 {
		return fmt.Errorf("supervisor refused to spawn %q: %s", argv[0], res)
	}

	return nil
}

// listen prepares the supervisor's socket.
//
// There is deliberately no authentication on it, and none should be added.
// Everything the supervisor will do, a process already inside this sandbox
// can do for itself, so a credential would only protect the sandbox from
// code that is already running in it. What the socket must not be is
// reachable from another workload, and that is a question of where it
// sits, not of what it asks for: files.WorkloadAgentDir gives each
// workload a directory of its own, outside the runtime directory a profile
// shares with every sibling, and only that directory is bound into this
// sandbox. Read that as the check, because there is none here.
func listen(socket string) (net.Listener, error) {
	if len(socket) >= maxSocketPath {
		return nil, fmt.Errorf("sandbox: socket path %q is over the %d byte limit",
			socket, maxSocketPath-1)
	}

	// A socket file left behind by a sandbox that is gone would refuse the
	// bind. Unlinking one a live supervisor is listening on would leave
	// that supervisor unreachable, but a second sandbox for one workload
	// is only ever started once the host has found the first unreachable
	// already.
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("sandbox: failed to clear the supervisor socket: %w", err)
	}

	lc := net.ListenConfig{}

	ln, err := lc.Listen(context.Background(), "unix", socket)
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to listen on the supervisor socket: %w", err)
	}

	return ln, nil
}

// supervisor serves spawn requests for one sandbox.
type supervisor struct {
	// spawned counts the siblings still being waited for. Nothing outside
	// the tests reads it. See reap for why the waiting matters.
	spawned sync.WaitGroup
}

func (s *supervisor) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// The listener is closed when the main command exits, and that
			// is the only way out of here.
			slog.Debug("supervisor stopped accepting", "error", err)
			return
		}

		s.handle(conn)
	}
}

// handle reads one spawn request and answers it.
//
// Requests are served one at a time. Each is a fork and an exec, the only
// caller is the host and it makes one call per launch, so there is nothing
// to gain from overlapping them and one less thing to get wrong. The
// deadline is what keeps a caller that stops talking from holding the
// queue.
func (s *supervisor) handle(conn net.Conn) {
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(exchangeTimeout)); err != nil {
		slog.Warn("failed to set a deadline on a spawn request", "error", err)
		return
	}

	req, err := readFrame(conn)
	if err != nil {
		slog.Warn("failed to read a spawn request", "error", err)
		return
	}

	argv, err := decodeArgv(req)
	if err != nil {
		reply(conn, err)
		return
	}

	slog.Debug("spawning a sibling", "argv", argv)

	cmd, err := start(argv)
	if err != nil {
		reply(conn, err)
		return
	}

	s.reap(cmd)
	reply(conn, nil)
}

// reap waits for a spawned sibling.
//
// bwrap installs a reaper at pid 1 of the namespace it unshares, but that
// reaper is not this process. Without --as-pid-1, which qubesome does not
// pass, pid 1 is bwrap's own init and the supervisor is its first child,
// so a sibling started here is a child of the supervisor and not of the
// reaper. It only becomes an orphan the reaper collects if the supervisor
// dies first, which is the moment the whole sandbox ends anyway. So the
// supervisor waits for its own children, or a long session holds a zombie
// for every window that was ever closed.
func (s *supervisor) reap(cmd *execabs.Cmd) {
	s.spawned.Add(1)

	go func() {
		defer s.spawned.Done()

		if err := cmd.Wait(); err != nil {
			slog.Debug("a spawned sibling exited", "error", err)
		}
	}()
}

// start runs argv with the sandbox's own standard streams.
//
// A sibling shares them with the main command, which is what the container
// runner's exec did, and it is the only place its output can go: the
// sandbox has no terminal of its own.
func start(argv []string) (*execabs.Cmd, error) {
	cmd := execabs.Command(argv[0], argv[1:]...) //nolint:gosec // the command is the workload's own, from its configuration.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: failed to start %q: %w", argv[0], err)
	}

	return cmd, nil
}

// reply answers a spawn request. An empty payload is a process that
// started, and anything else is the reason it did not.
func reply(conn net.Conn, cause error) {
	var payload []byte
	if cause != nil {
		payload = []byte(cause.Error())
	}

	if err := writeFrame(conn, payload); err != nil {
		slog.Warn("failed to answer a spawn request", "error", err)
	}
}

// The protocol is one frame each way: a 32 bit big endian length followed
// by that many bytes. The request carries the argv, each element NUL
// terminated, which is how bwrap's own --args descriptor carries an
// argument list and what keeps an argument that is not valid UTF-8 intact,
// where JSON would silently rewrite it. Nothing else is exchanged, so
// neither end needs a schema to agree on.

func writeFrame(w io.Writer, payload []byte) error {
	if len(payload) > maxFrame {
		return fmt.Errorf("sandbox: a message of %d bytes is over the %d byte limit",
			len(payload), maxFrame)
	}

	buf := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(payload))) //nolint:gosec // the length is bounded by maxFrame above.
	copy(buf[4:], payload)

	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("sandbox: failed to write a message: %w", err)
	}

	return nil
}

func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("sandbox: failed to read a message length: %w", err)
	}

	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrame {
		return nil, fmt.Errorf("sandbox: a message of %d bytes is over the %d byte limit",
			n, maxFrame)
	}

	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("sandbox: failed to read a message: %w", err)
	}

	return payload, nil
}

func encodeArgv(argv []string) []byte {
	n := 0
	for _, a := range argv {
		n += len(a) + 1
	}

	b := make([]byte, 0, n)
	for _, a := range argv {
		b = append(b, a...)
		b = append(b, 0)
	}

	return b
}

func decodeArgv(b []byte) ([]string, error) {
	if len(b) == 0 {
		return nil, errors.New("sandbox: the spawn request carries no command")
	}
	if b[len(b)-1] != 0 {
		return nil, errors.New("sandbox: the spawn request is not NUL terminated")
	}

	parts := bytes.Split(b[:len(b)-1], []byte{0})

	argv := make([]string, 0, len(parts))
	for _, p := range parts {
		argv = append(argv, string(p))
	}

	if argv[0] == "" {
		return nil, errors.New("sandbox: the spawn request has an empty command")
	}

	return argv, nil
}
