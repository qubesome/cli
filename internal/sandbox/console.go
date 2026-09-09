package sandbox

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/execabs"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// ConsoleCommand is the qubesome subcommand that attaches a terminal to a
// machine. It is named here because both ends need it: a workload puts it
// on its command line and the cli registers a command under it.
const ConsoleCommand = "console"

// VMConsolePort is the vsock port a guest answers console connections on.
//
// It is a second port rather than a second kind of request on
// VMSupervisorPort because the two are served differently. A spawn is a
// fork and an exec and the supervisor serves them one at a time, while a
// console holds its connection for as long as the user keeps the terminal
// open. Sharing one accept loop would have the first console block every
// launch behind it.
const VMConsolePort uint32 = 1025

// A console connection opens exactly as a spawn does, with the argv frame
// and the answer to it, and then keeps the connection. What crosses it
// afterwards is a supervisor frame whose payload begins with a kind byte:
//
//	0 data     the rest of the payload is stream bytes, in either direction
//	1 winsize  four bytes, rows then columns, each 16 bit big endian
//	2 exit     one byte, the status a shell would report
//
// One connection carries all three, rather than a control connection
// beside a data one, because a resize and an exit status only mean
// anything against the stream they belong to. Two connections would have
// to carry an identifier to say which stream that is, and the identifier
// would then have to be allocated, agreed and checked.
type consoleKind byte

const (
	consoleData consoleKind = iota
	consoleWinSize
	consoleExit
)

const (
	// winSizeLen is the encoded length of a window size, two 16 bit
	// numbers.
	winSizeLen = 4

	// exitLen is the encoded length of an exit status. A status is what a
	// shell reports and that is a byte, with 128 plus the signal number
	// standing in for a process that was killed.
	exitLen = 1

	// consoleBuf is how much of either stream is carried in one frame. It
	// is a read size and not a limit on anything: a larger read is split
	// over frames and arrives the same.
	consoleBuf = 32 * 1024

	// consoleDrain bounds the wait for the last of a command's output
	// after it has exited.
	//
	// The output is already in the pty's buffer at that point, which is
	// tens of kilobytes, so this is generous. What it is really for is
	// the case where the wait would otherwise never end: a process the
	// command left behind holds the slave open and the master never
	// reports the stream as finished.
	consoleDrain = 250 * time.Millisecond

	// ptmxPath is the pty multiplexor a guest allocates consoles through.
	ptmxPath = "/dev/ptmx"

	// ptsDir is where the slave of a pty appears.
	ptsDir = "/dev/pts/"

	// consoleGrace bounds how long a machine that was given no command
	// waits for its first console.
	//
	// It is minutes rather than seconds because a console is a workload
	// of its own. The host boots the machine and then builds a terminal
	// sandbox beside it, and the first launch of that terminal pulls and
	// unpacks an image before anything dials.
	consoleGrace = 5 * time.Minute
)

// consoleGate is the lifetime of a machine whose whole job is to accept
// consoles.
//
// A firecracker workload is allowed to leave command empty, and both of
// the ones in the reference configuration do, because what such a
// machine is for is the consoles that attach to it and each of those
// brings an argv of its own. There is no main command whose exit ends
// the machine, so the consoles have to say when it is finished.
//
// It ends on the first of two things. Either a console attached and the
// last one has now closed, which is the machine having done what it was
// booted for, or nothing attached within consoleGrace. The second is
// what keeps a machine started by mistake from holding its memory
// reservation until the host is rebooted, since qubesome has no command
// that stops one.
type consoleGate struct {
	mu sync.Mutex

	// open is how many consoles are attached and seen records whether
	// one ever was. Both are needed: an open count of zero is the state
	// a machine boots into as well as the state it finishes in.
	open int
	seen bool

	ended bool
	done  chan struct{}
}

func newConsoleGate(grace time.Duration) *consoleGate {
	g := &consoleGate{done: make(chan struct{})}

	// There is nothing to cancel when a console does attach, because
	// giveUp answers that case by doing nothing.
	time.AfterFunc(grace, g.giveUp)

	return g
}

// enter records a console that has attached.
func (g *consoleGate) enter() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.seen = true
	g.open++
}

// leave records a console that has closed, and finishes the machine when
// it was the last one.
func (g *consoleGate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.open--
	if g.open <= 0 {
		g.end()
	}
}

// giveUp finishes a machine no console ever attached to.
func (g *consoleGate) giveUp() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.seen {
		return
	}

	slog.Info("no console attached to the machine, shutting it down")

	g.end()
}

// end is called with the lock held. Both callers can be the one that
// finishes the machine, and closing a closed channel panics.
func (g *consoleGate) end() {
	if g.ended {
		return
	}

	g.ended = true
	close(g.done)
}

// wait blocks until the machine has no reason left to be up.
func (g *consoleGate) wait() {
	<-g.done
}

// superviseConsoles serves a machine that was given no command.
//
// It is superviseWith with the main command taken out. Spawn requests
// are still answered, since something may be started into the machine
// from outside it, but a sibling does not extend a machine's life any
// more than it extends a sandbox's. The gate does. See consoleGate.
func superviseConsoles(ln net.Listener, st starter, gate *consoleGate) error {
	defer ln.Close()

	s := &supervisor{starter: st}
	go s.serve(ln)

	gate.wait()

	return nil
}

// winSize is a terminal's size in character cells.
type winSize struct {
	Rows uint16
	Cols uint16
}

// consoleFrame builds the payload of one console frame.
func consoleFrame(kind consoleKind, body []byte) []byte {
	payload := make([]byte, 0, 1+len(body))
	payload = append(payload, byte(kind))

	return append(payload, body...)
}

// splitConsoleFrame reads the kind off a console frame's payload.
func splitConsoleFrame(payload []byte) (consoleKind, []byte, error) {
	if len(payload) == 0 {
		return 0, nil, errors.New("sandbox: a console frame carries no kind")
	}

	return consoleKind(payload[0]), payload[1:], nil
}

func encodeWinSize(ws winSize) []byte {
	b := make([]byte, winSizeLen)
	binary.BigEndian.PutUint16(b[:2], ws.Rows)
	binary.BigEndian.PutUint16(b[2:], ws.Cols)

	return b
}

func decodeWinSize(body []byte) (winSize, error) {
	if len(body) != winSizeLen {
		return winSize{}, fmt.Errorf("sandbox: a window size of %d bytes, want %d", len(body), winSizeLen)
	}

	return winSize{
		Rows: binary.BigEndian.Uint16(body[:2]),
		Cols: binary.BigEndian.Uint16(body[2:]),
	}, nil
}

// encodeExitStatus carries a status a shell would report.
//
// Anything outside a byte is clamped rather than truncated. A status only
// leaves that range through a bug, and truncating one would turn a
// failure into a success, which is the one answer that must never be
// invented.
func encodeExitStatus(status int) []byte {
	switch {
	case status < 0:
		return []byte{1}
	case status > math.MaxUint8:
		return []byte{math.MaxUint8}
	default:
		return []byte{byte(status)}
	}
}

func decodeExitStatus(body []byte) (int, error) {
	if len(body) != exitLen {
		return 0, fmt.Errorf("sandbox: an exit status of %d bytes, want %d", len(body), exitLen)
	}

	return int(body[0]), nil
}

// exitStatus reports what a shell would for a process that ended this
// way. A process killed by a signal has no exit status of its own, and
// 128 plus the signal number is what every shell reports instead.
func exitStatus(ws syscall.WaitStatus) int {
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}

	return ws.ExitStatus()
}

// consoleWriter serialises what a console sends.
//
// Both ends have two writers on one connection: a guest sends the
// command's output while it is also sending the exit status, and a host
// sends the keystrokes while it is also sending window sizes. A frame is
// one Write, so the lock is all it takes to keep two of them from
// interleaving into a payload neither end can read.
type consoleWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (c *consoleWriter) send(kind consoleKind, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return writeFrame(c.w, consoleFrame(kind, body))
}

// pumpData reads a stream and sends it on as data frames, until either
// end of it stops.
func pumpData(w *consoleWriter, r io.Reader) {
	buf := make([]byte, consoleBuf)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			if werr := w.send(consoleData, buf[:n]); werr != nil {
				slog.Debug("a console stopped sending", "error", werr)

				return
			}
		}

		if err != nil {
			return
		}
	}
}

// ConsoleVM attaches a terminal to the supervisor of a VM and returns the
// status of what it ran.
//
// It runs inside the sandbox of the workload that asked for the console,
// not on the host. uds is firecracker's host side vsock socket as that
// sandbox sees it, which is the only part of the machine bound into it.
// See files.InVMConsoleSocket.
func ConsoleVM(uds string, port uint32, argv []string) (int, error) {
	if len(argv) == 0 {
		return 0, errors.New("sandbox: a console needs a command to run")
	}

	conn, err := dialVM(uds, port)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrNoSupervisor, err)
	}
	defer conn.Close()

	// The opening is a spawn's, exactly. What makes this a console is
	// the port it was asked for and what crosses the connection next.
	if err := openRequest(conn, argv); err != nil {
		return 0, err
	}

	return console(conn, os.Stdin, os.Stdout)
}

// console carries one session between a terminal and a connection that
// has already been answered.
func console(conn net.Conn, in *os.File, out io.Writer) (int, error) {
	// The exchange bounded itself with a deadline, because a supervisor
	// that stops answering is one that has gone. A session has no bound:
	// it lasts as long as the user keeps the terminal open.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return 0, fmt.Errorf("sandbox: failed to clear the console deadline: %w", err)
	}

	restore, err := rawMode(in)
	if err != nil {
		return 0, err
	}

	// The terminal is put back on every way out of here, the panic
	// included, because a shell left in raw mode is the failure a user
	// remembers. Every goroutine below carries the same restore for the
	// same reason.
	defer restore()

	w := &consoleWriter{w: conn}

	stop := watchWinSize(in, w, restore)
	defer stop()

	go func() {
		defer restoreOnPanic(restore)

		pumpData(w, in)
	}()

	return readConsole(conn, out)
}

// readConsole reads the guest's half of a session until it reports how
// the command ended.
func readConsole(r io.Reader, out io.Writer) (int, error) {
	for {
		payload, err := readFrame(r)
		if err != nil {
			// A session that ends without a status is a machine or a
			// supervisor that went away under it. There is no status to
			// report, only the failure.
			return 0, fmt.Errorf("sandbox: the console ended with no exit status: %w", err)
		}

		kind, body, err := splitConsoleFrame(payload)
		if err != nil {
			return 0, err
		}

		switch kind {
		case consoleData:
			if _, err := out.Write(body); err != nil {
				return 0, fmt.Errorf("sandbox: failed to write the console's output: %w", err)
			}
		case consoleExit:
			return decodeExitStatus(body)
		case consoleWinSize:
			// Only a terminal has a size to report and the guest is not
			// one. Dropped rather than refused, so a supervisor that
			// grows a use for it does not have to be released alongside
			// every console.
			slog.Debug("a guest reported a window size, which nothing reads")
		default:
			return 0, fmt.Errorf("sandbox: the console was sent frame kind %d, which it does not know", kind)
		}
	}
}

// rawMode puts a terminal into the state a console needs and returns the
// way back.
//
// Raw mode is what puts every keystroke through unchanged. The interrupt,
// the suspend and the erase all belong to the line discipline of the
// guest's pty, and a local one acting on them first would mean the guest
// never saw them.
//
// The restore is idempotent, because it is deferred on the path that
// returns and called again by whichever goroutine recovers a panic.
//
// Input that is not a terminal is not an error. A console driven by a
// pipe is what the tests do, and it needs no mode change at all.
func rawMode(f *os.File) (func(), error) {
	fd := int(f.Fd())

	if !term.IsTerminal(fd) {
		return func() {}, nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to put the terminal into raw mode: %w", err)
	}

	var once sync.Once

	return func() {
		once.Do(func() {
			if err := term.Restore(fd, state); err != nil {
				slog.Warn("failed to restore the terminal", "error", err)
			}
		})
	}, nil
}

// restoreOnPanic puts the terminal back before a panic in a goroutine
// takes the process down with it.
//
// A deferred restore only runs on the goroutine that panics, and an
// unrecovered panic anywhere ends the process, so the restore deferred on
// the session's own goroutine would never run. The panic is raised again
// once the terminal is usable, because hiding it would be worse than the
// terminal was.
func restoreOnPanic(restore func()) {
	if r := recover(); r != nil {
		restore()
		panic(r)
	}
}

// watchWinSize reports the terminal's size to the guest, now and on every
// change, and returns the way to stop.
//
// SIGWINCH is the only notification there is. Nothing reaches the guest's
// pty on its own, so a terminal that is resized and does not say so
// leaves every full screen program in the machine drawing at the old
// size.
func watchWinSize(f *os.File, w *consoleWriter, restore func()) func() {
	fd := int(f.Fd())

	send := func() {
		cols, rows, err := term.GetSize(fd)
		if err != nil {
			// Not a terminal, or one that will not say. Neither is worth
			// ending a session over, and a guest that is told no size
			// keeps the pty's default.
			return
		}

		if err := w.send(consoleWinSize, encodeWinSize(winSize{Rows: cellCount(rows), Cols: cellCount(cols)})); err != nil {
			slog.Debug("failed to report the terminal size", "error", err)
		}
	}

	// The first size goes before anything runs, or the command starts
	// against the pty's own default of 0 by 0.
	send()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGWINCH)

	go func() {
		defer restoreOnPanic(restore)

		for range ch {
			send()
		}
	}()

	return func() {
		signal.Stop(ch)
		close(ch)
	}
}

// cellCount narrows a terminal dimension to what the wire carries. No
// terminal is anywhere near the limit, and clamping is what keeps a
// number that somehow is from wrapping to a small one.
func cellCount(n int) uint16 {
	switch {
	case n < 0:
		return 0
	case n > math.MaxUint16:
		return math.MaxUint16
	default:
		return uint16(n)
	}
}

// consoleStarter runs a console's command with a terminal of its own.
//
// It is separate from starter because the two answer different questions.
// A spawned sibling shares the sandbox's streams and its status is read
// by nobody, while a console's command is given a pty and its status is
// what the user's shell reports. Both are implemented twice for the same
// reason, once through os/exec and once through the machine's reaper. See
// starter.
type consoleStarter interface {
	startConsole(argv []string, tty *os.File) (statusWaiter, error)
}

// statusWaiter waits for a console's command and reports how it ended.
type statusWaiter interface {
	WaitStatus() int
}

// consoleCommand prepares argv against a pty.
func consoleCommand(argv []string, tty *os.File) *execabs.Cmd {
	cmd := execabs.Command(argv[0], argv[1:]...) //nolint:gosec // the command is the console's own, from its command line.
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

	// Setsid puts the command in a session of its own and Setctty makes
	// the pty that session's controlling terminal, which is what a shell
	// wants before it will turn job control on. Ctty is a descriptor
	// number in the child, and there the pty is 0, 1 and 2.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}

	return cmd
}

func (procStarter) startConsole(argv []string, tty *os.File) (statusWaiter, error) {
	cmd := consoleCommand(argv, tty)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: failed to start %q: %w", argv[0], err)
	}

	return &procConsole{cmd: cmd}, nil
}

// procConsole waits for a console's command through os/exec, as a child
// of this process.
type procConsole struct {
	cmd *execabs.Cmd
}

func (p *procConsole) WaitStatus() int {
	err := p.cmd.Wait()
	if err == nil {
		return 0
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
			return exitStatus(ws)
		}
	}

	// The wait itself failed, which says nothing about the command. A
	// shell reports a generic failure for the same case.
	slog.Debug("failed to wait for a console's command", "error", err)

	return 1
}

// serveConsoles answers console connections until the machine ends.
func serveConsoles(ln net.Listener, st consoleStarter, gate *consoleGate) {
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Debug("the guest stopped accepting consoles", "error", err)

			return
		}

		// The gate is entered here rather than in the goroutine below,
		// so that a console which attaches and closes at once cannot be
		// counted out before it was counted in.
		gate.enter()

		// A session lasts as long as the user keeps the terminal open, so
		// each gets a goroutine of its own. This is the difference from
		// supervisor.serve, which answers one request at a time because
		// each is only a fork and an exec.
		go func() {
			defer gate.leave()

			serveConsole(conn, st)
		}()
	}
}

// serveConsole answers one console connection and carries the session it
// opens.
func serveConsole(conn net.Conn, st consoleStarter) {
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(exchangeTimeout)); err != nil {
		slog.Warn("failed to set a deadline on a console request", "error", err)

		return
	}

	req, err := readFrame(conn)
	if err != nil {
		slog.Warn("failed to read a console request", "error", err)

		return
	}

	kind, body, err := splitRequest(req)
	if err != nil {
		reply(conn, err)

		return
	}
	if kind != requestSpawn {
		reply(conn, fmt.Errorf("sandbox: a console cannot serve request kind %d", kind))

		return
	}

	argv, err := decodeArgv(body)
	if err != nil {
		reply(conn, err)

		return
	}

	slog.Debug("opening a console", "argv", argv)

	ptmx, tty, err := openPTY()
	if err != nil {
		reply(conn, err)

		return
	}
	defer ptmx.Close()

	child, err := st.startConsole(argv, tty)

	// The slave is closed as soon as the command holds its own copy of
	// it. Keeping one here would keep the pty open after the command
	// ended, and the master would never report the stream as finished.
	_ = tty.Close()

	if err != nil {
		reply(conn, err)

		return
	}

	reply(conn, nil)

	if err := conn.SetDeadline(time.Time{}); err != nil {
		slog.Warn("failed to clear the deadline on a console", "error", err)

		return
	}

	relayConsole(conn, ptmx, child)
}

// relayConsole carries one session between a pty and a connection.
func relayConsole(conn net.Conn, ptmx *os.File, child statusWaiter) {
	w := &consoleWriter{w: conn}

	drained := make(chan struct{})

	go func() {
		defer close(drained)

		pumpData(w, ptmx)
	}()

	go func() {
		// There is no clean end to the host's half. It stops when the
		// connection does, so the reason is always worth having and is
		// never a success.
		slog.Debug("a console stopped reading", "error", consoleInput(conn, ptmx))

		// The host is gone, or said something a console does not say.
		// Closing the master hangs the pty up, which sends SIGHUP to the
		// session the command leads, so the wait below ends rather than
		// holding a terminal nobody is at.
		_ = ptmx.Close()
	}()

	status := child.WaitStatus()

	// The command's last output is in the pty's buffer and may not have
	// been read yet. See consoleDrain for what the deadline is really
	// guarding against.
	_ = ptmx.SetReadDeadline(time.Now().Add(consoleDrain))
	<-drained

	if err := w.send(consoleExit, encodeExitStatus(status)); err != nil {
		slog.Debug("failed to report a console's exit status", "error", err)
	}
}

// consoleInput applies the host's half of a session to the pty.
func consoleInput(r io.Reader, ptmx *os.File) error {
	for {
		payload, err := readFrame(r)
		if err != nil {
			return err
		}

		kind, body, err := splitConsoleFrame(payload)
		if err != nil {
			return err
		}

		switch kind {
		case consoleData:
			if _, err := ptmx.Write(body); err != nil {
				return fmt.Errorf("sandbox: failed to write to the console's terminal: %w", err)
			}
		case consoleWinSize:
			ws, err := decodeWinSize(body)
			if err != nil {
				return err
			}

			if err := resizePTY(ptmx, ws); err != nil {
				slog.Warn("failed to resize a console's terminal", "error", err)
			}
		case consoleExit:
			return errors.New("sandbox: a console sent an exit status, which only a supervisor sends")
		default:
			return fmt.Errorf("sandbox: a console sent frame kind %d, which the guest does not know", kind)
		}
	}
}

// resizePTY tells the pty how big the terminal at the other end is.
//
// The descriptor is reached through SyscallConn rather than through Fd,
// which would take the master out of the runtime poller by putting it
// back into blocking mode. The poller is what lets the read on it be
// ended by a deadline or by a close, and both are how a session is torn
// down.
func resizePTY(ptmx *os.File, ws winSize) error {
	rc, err := ptmx.SyscallConn()
	if err != nil {
		return err
	}

	var cause error

	if err := rc.Control(func(fd uintptr) {
		cause = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: ws.Rows, Col: ws.Cols})
	}); err != nil {
		return err
	}

	return cause
}

// openPTY allocates a pseudo terminal for one console.
//
// A console cannot inherit the supervisor's streams. Those are the
// machine's serial console, shared by everything in it and not a terminal
// any one session can put into a mode of its own.
//
// The three ioctls are the whole of a Linux pty allocation. TIOCGPTN
// names the slave that goes with this master, TIOCSPTLCK unlocks it, and
// the slave is then an ordinary open. creack/pty is these three calls,
// and x/sys/unix is already here.
//
// The master is opened non-blocking so that the runtime poller adopts it,
// which is what makes its deadline and its close work. The slave is not,
// because it becomes a command's standard streams and a command does not
// expect them to be non-blocking.
func openPTY() (*os.File, *os.File, error) {
	fd, err := unix.Open(ptmxPath, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("sandbox: failed to open %s: %w", ptmxPath, err)
	}

	master := os.NewFile(uintptr(fd), ptmxPath)

	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()

		return nil, nil, fmt.Errorf("sandbox: failed to name the pty slave: %w", err)
	}

	// The slave is locked from the moment the master is created, so that
	// nothing can open it before it has been set up.
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()

		return nil, nil, fmt.Errorf("sandbox: failed to unlock the pty slave: %w", err)
	}

	name := ptsDir + strconv.Itoa(n)

	sfd, err := unix.Open(name, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()

		return nil, nil, fmt.Errorf("sandbox: failed to open the pty slave %s: %w", name, err)
	}

	return master, os.NewFile(uintptr(sfd), name), nil
}
