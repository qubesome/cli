// Package session holds one user namespace open for as long as a qubesome
// session lasts, and launches sandboxes inside it.
//
// Every sandbox that needs to be reachable from the session's gateway is
// started as bwrap --userns <holder> --dev-bind / / -- bwrap <the sandbox
// itself>, so the sandbox's own user namespace is a child of the session's
// rather than the same one.
//
// Nesting rather than sharing is the whole design and it is worth stating
// why. bwrap refuses --disable-userns together with --userns, so a sandbox
// that joined the session namespace would lose that hardening. A joined
// namespace also fixes every sandbox to the holder's single uid mapping,
// so an image whose config says uid 0 could not start at all. And a
// capability reaches the namespaces below the one it is held in, so in one
// shared namespace a workload granted CAP_NET_ADMIN would hold it over
// every other workload's network namespace.
//
// Nesting keeps the reach the gateway actually needs. A process holding
// CAP_NET_ADMIN in the session namespace holds it in every descendant, so
// a helper running in the session namespace can put one end of a veth in a
// workload's network namespace and address it from outside, while the
// workload itself holds nothing over that namespace. Its address is its
// identity to the gateway, so it must not be able to change it.
package session

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
	"golang.org/x/sys/execabs"
	"golang.org/x/sys/unix"
)

// HoldCommand is the qubesome subcommand that holds the session's user
// namespace open. It is named here because both ends need it: the cli
// registers a command under it and Start puts it on the holder's command
// line. sandbox.SuperviseCommand is named the same way and for the same
// reason.
const HoldCommand = "session-hold"

var (
	// ErrHeld reports that another process already holds the session lock,
	// which means a holder is already running for this session.
	ErrHeld = errors.New("session: the lock is held by another holder")

	// ErrNoHolder reports that no holder is running, so there is no user
	// namespace for a sandbox to nest inside.
	ErrNoHolder = errors.New("session: no holder is running")
)

const (
	// holderGrace bounds the wait for a started holder to record itself.
	// Between the exec and the state file there is a bwrap setup and a Go
	// runtime start, and a caller that gave up inside that window would
	// start a second holder because the first was not quite up yet. It is
	// the same reasoning, and the same shape, as the startup grace the
	// bwrap runner waits for a supervisor's socket with.
	holderGrace = 5 * time.Second

	holderPoll = 20 * time.Millisecond
)

// Session names the files one session is kept in.
//
// The paths are carried rather than read from files at each use so that a
// test can drive a session outside the user's run directory.
type Session struct {
	Dir       string
	LockPath  string
	StatePath string
}

// Current returns the session of the user running qubesome.
func Current() Session {
	return Session{
		Dir:       files.SessionDir(),
		LockPath:  files.SessionLockPath(),
		StatePath: files.SessionStatePath(),
	}
}

// Hold keeps the session's user namespace open until the process is
// signalled.
//
// It is the body of the hidden subcommand and runs as the command of
// bwrap --unshare-user, whose namespace is the one being held. It opens
// nothing else and does no work, because everything that touches the
// session's namespaces reaches into it from outside rather than asking
// anything of it.
//
// The lock is taken here, in the process that is going to block, and not
// by whatever launched it. qubesome run at a terminal returns as soon as
// the sandbox is started, so a lock taken by the launcher would be
// released while the holder it started was still running, and a second
// session with a second namespace and a second gateway could start
// alongside the first. An flock lives on the open file description, so
// this one is released when this process exits and not before, by any
// route out including a signal it does not handle.
func (s Session) Hold() error {
	if err := os.MkdirAll(s.Dir, files.DirMode); err != nil {
		return fmt.Errorf("failed to create the session dir %q: %w", s.Dir, err)
	}

	lock, err := acquire(s.LockPath)
	if err != nil {
		return err
	}
	defer lock.Close()

	if err := sandbox.WriteState(s.StatePath, os.Getpid()); err != nil {
		return fmt.Errorf("failed to record the session holder: %w", err)
	}
	defer func() {
		if err := os.Remove(s.StatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("failed to remove the session state", "path", s.StatePath, "error", err)
		}
	}()

	slog.Debug("[session] holding the session user namespace", "pid", os.Getpid())

	// A holder that is killed outright leaves its state file behind, which
	// is what sandbox.Alive reads as not running. Handling the two signals
	// a shutdown normally arrives as removes the file in the ordinary case
	// instead of leaving it for the next launch to see through.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	return nil
}

// acquire takes the session lock and returns the file that holds it.
//
// LOCK_NB is what makes a second holder fail at once rather than queue
// behind the first for the length of a session.
//
// O_RDONLY because nothing is ever written here. The lock lives on the
// open file description and flock takes any descriptor, unlike an fcntl
// lock, which would need the access mode to match. Opening it writable
// made static analysis read a discarded Close as a lost write, which it
// could never be, and least privilege is the honest answer to that rather
// than handling an error that cannot happen.
func acquire(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, files.FileMode)
	if err != nil {
		return nil, fmt.Errorf("failed to open the session lock %q: %w", path, err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()

		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, fmt.Errorf("failed to lock %q: %w", path, err)
	}

	return f, nil
}

// Start makes sure a holder is running for this session and returns once
// one is recorded.
//
// Two launches racing here both start a holder, and the lock settles it.
// The loser exits without recording anything, and the state file the
// winner wrote is the one both callers go on to use.
func (s Session) Start() error {
	if sandbox.Alive(s.StatePath) {
		return nil
	}

	if err := os.MkdirAll(s.Dir, files.DirMode); err != nil {
		return fmt.Errorf("failed to create the session dir %q: %w", s.Dir, err)
	}

	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find the qubesome binary: %w", err)
	}

	cmd := execabs.Command(files.BwrapBinary, //nolint:gosec // every argument is a constant apart from this binary's own path.
		"--unshare-user", "--dev-bind", "/", "/", "--", bin, HoldCommand)

	// The holder outlives whatever started it. A qubesome run typed at a
	// terminal sits in the shell's foreground process group, and a session
	// that ended at the first Ctrl-C would take with it the parent
	// namespace of every sandbox still to be started under it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	// A holder that fails to start says why here, as a workload sandbox
	// does. Its stdin stays closed, since nothing reads from it and the
	// shell has taken the terminal back.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the session holder: %w", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	return s.wait(exited)
}

// wait blocks until the holder records itself, exits, or runs out of
// grace.
func (s Session) wait(exited <-chan error) error {
	deadline := time.Now().Add(holderGrace)

	for {
		if sandbox.Alive(s.StatePath) {
			return nil
		}

		select {
		case err := <-exited:
			// The holder that lost the race for the lock exits straight
			// away, and the one that won it is already recorded.
			if sandbox.Alive(s.StatePath) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("the session holder exited before it was recorded: %w", err)
			}
			return errors.New("the session holder exited before it was recorded")
		default:
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for the session holder to start", holderGrace)
		}

		time.Sleep(holderPoll)
	}
}

// Namespace is an open handle on a session's user namespace, ready to be
// handed to bwrap.
type Namespace struct {
	f  *os.File
	fd int
}

// Open returns a handle on the user namespace of this session's holder.
//
// fd is the descriptor number the handle will have in the child, which
// os/exec numbers from 3 upwards in the order of exec.Cmd.ExtraFiles. It is
// given rather than worked out here for the reason sandbox.PackArgs takes
// one too: only the caller knows what else it is passing down.
func (s Session) Open(fd int) (*Namespace, error) {
	if fd < 3 {
		return nil, fmt.Errorf("session: namespace descriptor %d collides with the standard streams", fd)
	}

	// A state file naming a pid that is gone, or one a different process
	// has since been given, reads as not running here. Without the check
	// the pid would be opened anyway and the sandbox would nest inside
	// whatever namespace that process happens to be in.
	if !sandbox.Alive(s.StatePath) {
		return nil, ErrNoHolder
	}

	st, err := sandbox.ReadState(s.StatePath)
	if err != nil {
		return nil, err
	}

	path := "/proc/" + strconv.Itoa(st.PID) + "/ns/user"

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open the session user namespace: %w", err)
	}

	return &Namespace{f: f, fd: fd}, nil
}

// File returns the descriptor to pass in exec.Cmd.ExtraFiles. It has to
// land at the position Open was given.
func (n *Namespace) File() *os.File {
	return n.f
}

// Close releases the handle on the session's user namespace. The namespace
// itself stays open, since the holder is what keeps it.
func (n *Namespace) Close() error {
	return n.f.Close()
}

// Enter wraps a bwrap argument list so the sandbox it describes is created
// inside the session's user namespace.
//
// args is everything after the bwrap binary of the sandbox as it would
// have been launched on its own, and the result is everything after the
// bwrap binary of the outer invocation. A caller wraps a launch by passing
// its argument list through here and changing nothing else about it.
//
// The outer bwrap gets --dev-bind / / rather than --bind / /, and rather
// than no mount at all. With no mount it cannot find the binary it is
// asked to run. With --bind the inner sandbox's own --dev-bind of a camera
// or a security key has no device node left to reach.
//
// Descriptors pass through the outer bwrap unchanged, which is what lets
// the inner sandbox go on naming the seccomp filter and the packed
// argument file by number. That looks like something that would not
// survive a second bwrap, so it was measured rather than assumed: with
// bubblewrap 0.11.2 on 2026-09-08, a file opened on descriptor 3 outside
// was still readable from a shell inside a nested bwrap.
func (n *Namespace) Enter(args []string) []string {
	const prefix = 7

	out := make([]string, 0, prefix+len(args))
	out = append(out,
		"--userns", strconv.Itoa(n.fd),
		"--dev-bind", "/", "/",
		"--", files.BwrapBinary)

	return append(out, args...)
}
