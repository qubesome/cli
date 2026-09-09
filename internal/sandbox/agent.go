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

// GatedFlag is the first argument of a supervisor that must wait for the
// host before it starts the workload.
//
// It is a positional token rather than a parsed flag because the supervise
// subcommand skips flag parsing: everything after the command name belongs
// to the workload, and most workloads pass flags of their own. qubesome
// writes this argument list itself, so nothing else can arrive in that
// position.
const GatedFlag = "--gated"

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

	// gateGrace bounds how long a gated supervisor waits to be released.
	//
	// Behind the release are a pid read, three short helper processes that
	// build and address the sandbox's veth, and one call to the gateway.
	// None of them pulls anything, because the gateway was up and ready
	// before the sandbox was started, so this is a ceiling rather than an
	// estimate. What it really bounds is the host going away between
	// starting the sandbox and releasing it, which leaves nothing else to
	// end the wait.
	gateGrace = 60 * time.Second
)

// Supervisor requests begin with a kind byte, as a console frame's payload
// does:
//
//	0 spawn    the rest is the argv, each element NUL terminated
//	1 release  the rest is empty, and it opens a gated supervisor's gate
//
// One kind on the connection the supervisor already listens on, rather than
// a second channel: the release says the same thing to the same process at
// the same socket, and a second socket would be a second thing to place,
// bind and bind into the sandbox.
type requestKind byte

const (
	requestSpawn requestKind = iota
	requestRelease
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
//
// gated says the workload must not start until the host says so. A
// workload given a gateway address is launched that way, so its first name
// lookup cannot precede the resolver it is meant to reach.
func Supervise(socket string, argv []string, gated bool) error {
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

	return supervise(ln, argv, gated)
}

// supervise runs argv and serves requests on ln until argv exits, with the
// processes waited for as ordinary children of this one.
//
// It is the half of Supervise that does not know what it is listening on.
// A sandbox in a VM has no unix socket to be reached on and reuses
// superviseWith with a vsock listener and a starter of its own. See
// SuperviseVM.
func supervise(ln net.Listener, argv []string, gated bool) error {
	return superviseWith(ln, argv, procStarter{}, newGate(gated))
}

// superviseWith is supervise with the transport and the way processes are
// waited for both left to the caller.
//
// The two are separate questions and the VM answers both differently: it
// is reached over vsock rather than over a unix socket, and it is pid 1,
// so it cannot wait for a process by pid. See guestReaper.
func superviseWith(ln net.Listener, argv []string, st starter, g *gate) error {
	defer ln.Close()

	s := &supervisor{starter: st, gate: g}

	// Serving comes before the main command, because on a gated supervisor
	// the release that starts that command arrives here.
	go s.serve(ln)

	// An open gate returns at once, which is every supervisor that was not
	// asked to wait. A gate that expires returns an error and the main
	// command is never started: the sandbox ends instead of running a
	// workload the gateway was never told about, which is the same
	// fail-closed rule the gateway's own lifecycle follows.
	if err := g.wait(); err != nil {
		return err
	}

	main, err := st.start(argv)
	if err != nil {
		return err
	}

	// The sandbox's lifetime is the main command's, exactly as the
	// container's was. Returning ends the sandbox: under bwrap this
	// process is the first child of the unshared pid namespace, so the
	// kernel tears that namespace down, and in a VM it is pid 1 and the
	// caller shuts the machine down. Either way every sibling spawned
	// into it goes with it. Siblings are deliberately neither waited for
	// nor signalled first: a sibling is another window of the same
	// application, and the container ended them the same way when its
	// main process exited.
	return main.Wait()
}

// gate holds a workload back until the host has given it its address.
type gate struct {
	// released is closed by the release request. A nil gate is an open one
	// and is the ordinary case, so the zero value of the field on
	// supervisor is a supervisor that waits for nothing.
	released chan struct{}

	// once keeps a second release from closing a closed channel. Nothing
	// sends two, and a supervisor that panicked on one would take the
	// sandbox with it.
	once sync.Once

	grace time.Duration
}

// newGate returns the gate a supervisor waits on, or nil when it waits for
// nothing.
func newGate(gated bool) *gate {
	if !gated {
		return nil
	}

	return &gate{released: make(chan struct{}), grace: gateGrace}
}

// wait blocks until the gate is opened or its grace runs out. A nil gate
// was never closed, so it returns at once.
func (g *gate) wait() error {
	if g == nil {
		return nil
	}

	t := time.NewTimer(g.grace)
	defer t.Stop()

	select {
	case <-g.released:
		return nil
	case <-t.C:
		return fmt.Errorf(
			"sandbox: the workload was not given its gateway address within %s, so it was not started", g.grace)
	}
}

// open releases the gate, and reports whether there was one to release.
//
// A release arriving at a supervisor that was not gated is refused rather
// than ignored. It means the host thinks this workload has an address and
// this sandbox was never told to wait for one, and the two disagreeing is
// worth reporting where it happens.
func (g *gate) open() error {
	if g == nil {
		return errors.New("sandbox: this supervisor has no gate to release")
	}

	g.once.Do(func() { close(g.released) })

	return nil
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

	conn, err := dial(socket)
	if err != nil {
		return err
	}

	return spawn(conn, argv)
}

// Release tells the gated supervisor listening on socket that its workload
// has its gateway address and may start.
//
// It is the last step of a launch that gives a workload egress, and until
// it arrives the sandbox holds an application that has never run. Every
// failure before it stops the launch and kills the sandbox instead.
func Release(socket string) error {
	conn, err := dial(socket)
	if err != nil {
		return err
	}
	defer conn.Close()

	return exchange(conn, requestRelease, nil, "release")
}

func dial(socket string) (net.Conn, error) {
	d := net.Dialer{Timeout: exchangeTimeout}

	conn, err := d.DialContext(context.Background(), "unix", socket)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}

	return conn, nil
}

// spawn asks the supervisor at the other end of conn to start argv.
//
// It takes a connection rather than an address because the two transports
// reach a supervisor in different ways and say the same thing once they
// have. See SpawnVM for the other way.
func spawn(conn net.Conn, argv []string) error {
	defer conn.Close()

	return openRequest(conn, argv)
}

// openRequest asks the supervisor at the other end of conn to start argv.
//
// It is every connection's opening, a console's included: a console says
// exactly this and then keeps the connection to carry the terminal, which
// is why it does not close conn. See ConsoleVM.
func openRequest(conn net.Conn, argv []string) error {
	return exchange(conn, requestSpawn, encodeArgv(argv), argv[0])
}

// exchange sends one request to the supervisor at the other end of conn
// and reads its answer. The close is the caller's, since a console keeps
// the connection it opened.
//
// what names the request in a refusal, since the payload of one is not
// always something to put in a message.
func exchange(conn net.Conn, kind requestKind, body []byte, what string) error {
	if err := conn.SetDeadline(time.Now().Add(exchangeTimeout)); err != nil {
		return fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}

	if err := writeFrame(conn, request(kind, body)); err != nil {
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
		return fmt.Errorf("supervisor refused %q: %s", what, res)
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

// starter runs a process for the supervisor and hands back the way to
// wait for it.
//
// It exists because who is allowed to wait for a process depends on where
// the sandbox is. Under bwrap the supervisor is an ordinary process and
// os/exec waits for its own children. In a VM the supervisor is pid 1 and
// a single wait4 loop owns every exit status in the machine, so a caller
// is handed a delivery rather than a wait of its own.
type starter interface {
	start(argv []string) (waiter, error)
}

// waiter waits for one process the supervisor started and reports how it
// ended.
type waiter interface {
	Wait() error
}

// procStarter waits for a process through os/exec, as a child of this
// one. It is the arrangement every sandbox but a VM's is in.
type procStarter struct{}

func (procStarter) start(argv []string) (waiter, error) {
	return start(argv)
}

// supervisor serves requests for one sandbox.
type supervisor struct {
	// starter runs the siblings. See the interface for why it is not
	// always os/exec.
	starter starter

	// gate is what a release request opens, or nil for a supervisor that
	// was not asked to wait for one.
	gate *gate

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

// handle reads one request and answers it.
//
// Requests are served one at a time. A spawn is a fork and an exec and a
// release is closing a channel, the only caller is the host and it makes
// one call per launch, so there is nothing to gain from overlapping them
// and one less thing to get wrong. The deadline is what keeps a caller
// that stops talking from holding the queue.
func (s *supervisor) handle(conn net.Conn) {
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(exchangeTimeout)); err != nil {
		slog.Warn("failed to set a deadline on a supervisor request", "error", err)
		return
	}

	req, err := readFrame(conn)
	if err != nil {
		slog.Warn("failed to read a supervisor request", "error", err)
		return
	}

	kind, body, err := splitRequest(req)
	if err != nil {
		reply(conn, err)
		return
	}

	switch kind {
	case requestSpawn:
		s.handleSpawn(conn, body)
	case requestRelease:
		slog.Debug("releasing the workload")
		reply(conn, s.gate.open())
	default:
		reply(conn, fmt.Errorf("sandbox: unknown request kind %d", kind))
	}
}

func (s *supervisor) handleSpawn(conn net.Conn, body []byte) {
	argv, err := decodeArgv(body)
	if err != nil {
		reply(conn, err)
		return
	}

	slog.Debug("spawning a sibling", "argv", argv)

	child, err := s.starter.start(argv)
	if err != nil {
		reply(conn, err)
		return
	}

	s.reap(child)
	reply(conn, nil)
}

// reap waits for a spawned sibling, so that a long session does not hold
// a zombie for every window that was ever closed.
//
// Under bwrap the wait has to happen here. bwrap installs a reaper at pid
// 1 of the namespace it unshares, but that reaper is not this process.
// Without --as-pid-1, which qubesome does not pass, pid 1 is bwrap's own
// init and the supervisor is its first child, so a sibling started here
// is a child of the supervisor and not of the reaper. It only becomes an
// orphan the reaper collects if the supervisor dies first, which is the
// moment the whole sandbox ends anyway.
//
// In a VM there is no reaper above this process, because this process is
// the reaper. Being pid 1 makes the job harder rather than easier and the
// waiting is arranged differently underneath, but from here it reads the
// same. See guestReaper.
func (s *supervisor) reap(child waiter) {
	s.spawned.Add(1)

	go func() {
		defer s.spawned.Done()

		if err := child.Wait(); err != nil {
			slog.Debug("a spawned sibling exited", "error", err)
		}
	}()
}

// command prepares argv with the sandbox's own standard streams.
//
// A sibling shares them with the main command, which is what the container
// runner's exec did, and it is the only place its output can go: the
// sandbox has no terminal of its own.
func command(argv []string) *execabs.Cmd {
	cmd := execabs.Command(argv[0], argv[1:]...) //nolint:gosec // the command is the workload's own, from its configuration.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}

// start runs argv as a child of this process.
func start(argv []string) (*execabs.Cmd, error) {
	cmd := command(argv)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: failed to start %q: %w", argv[0], err)
	}

	return cmd, nil
}

// request builds the payload of one supervisor request: the kind byte and
// whatever that kind carries.
func request(kind requestKind, body []byte) []byte {
	payload := make([]byte, 0, 1+len(body))
	payload = append(payload, byte(kind))

	return append(payload, body...)
}

// splitRequest reads the kind off a request's payload.
func splitRequest(payload []byte) (requestKind, []byte, error) {
	if len(payload) == 0 {
		return 0, nil, errors.New("sandbox: a supervisor request carries no kind")
	}

	return requestKind(payload[0]), payload[1:], nil
}

// reply answers a request. An empty payload is a request that was carried
// out, and anything else is the reason it was not.
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
// by that many bytes. The request begins with a kind byte, and a spawn's
// carries the argv after it, each element NUL terminated, which is how
// bwrap's own --args descriptor carries an argument list and what keeps an
// argument that is not valid UTF-8 intact, where JSON would silently
// rewrite it. Nothing else is exchanged, so neither end needs a schema to
// agree on.

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
