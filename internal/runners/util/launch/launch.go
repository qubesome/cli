// Package launch starts a bwrap sandbox, optionally nested inside the
// session's user namespace so that a veth can be wired into it.
//
// It is shared by the two runners. A workload sandbox and the sandbox a
// microVM's VMM runs in are given entirely different things, and are
// started in exactly the same way, and the nesting in particular is subtle
// enough that one copy of it is the right number.
package launch

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/runners/util/container"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/seccomp"
	"github.com/qubesome/cli/internal/session"
	"golang.org/x/sys/execabs"
)

// firstExtraFD is the descriptor os/exec puts the first ExtraFiles entry
// on in the child.
const firstExtraFD = 3

const (
	// StartupGrace bounds the wait for a sandbox that was recorded a
	// moment ago to reach the point of listening. Between the host
	// recording the sandbox and the supervisor binding its socket there is
	// a bwrap setup and an exec, and a caller that gave up inside that
	// window would read a sandbox that is not quite up yet as one that is
	// not there.
	StartupGrace = 2 * time.Second

	StartupPoll = 50 * time.Millisecond
)

// Release opens a gated supervisor's gate, waiting for a sandbox that is
// still starting.
//
// The wait is there because between the sandbox existing and the supervisor
// binding its socket there is a bwrap setup and an exec. The sandbox is
// known to exist by the time this is called, since its pid was read from
// bwrap, so what is being waited for is only the socket.
func Release(socket string) error {
	deadline := time.Now().Add(StartupGrace)

	for {
		err := sandbox.Release(socket)
		if !errors.Is(err, sandbox.ErrNoSupervisor) || time.Now().After(deadline) {
			return err
		}

		time.Sleep(StartupPoll)
	}
}

// Launcher is one prepared sandbox, before and after it starts.
//
// A workload taking a gateway address is started differently from one that
// is not, and the difference is not only the arguments. It nests inside the
// session's user namespace, so there is an outer bwrap between this process
// and the sandbox, and it reports its own pid on an info descriptor so the
// veth has somewhere to go. This holds both shapes, so the launch reads the
// same either way and only the failure handling has to know which it is.
type Launcher struct {
	cmd *execabs.Cmd

	// files are the descriptors handed to the sandbox, closed once it has
	// its own copies. The namespace handle is among them, which is why they
	// are held rather than closed at the point each was opened.
	files []*os.File

	// info is the read end of bwrap's --info-fd, or nil for a sandbox that
	// was not asked to report its pid.
	info *os.File

	// pid is the sandbox's own init process in the host's pid namespace,
	// once it has been read. It is not cmd.Process.Pid: for a nested
	// launch that is the outer bwrap, and killing it does not signal the
	// sandbox below.
	pid int
}

// New prepares the sandbox described by spec.
//
// gated says the sandbox is to take a gateway address, which is what
// decides both of the differences above.
func New(spec sandbox.Spec, gated bool) (*Launcher, error) {
	l := &Launcher{}

	seccompFD := -1

	if spec.Seccomp {
		filter, err := seccomp.MemFD()
		if err != nil {
			l.Close()

			return nil, err
		}

		seccompFD = firstExtraFD + len(l.files)
		l.files = append(l.files, filter)
	}

	args, err := sandbox.Args(spec, seccompFD)
	if err != nil {
		l.Close()

		return nil, err
	}

	// A mime enabled workload carries the profile's mTLS private key in
	// its environment, and a command line is world readable through
	// /proc. Only the descriptor holding the options, and the command,
	// stay on it.
	outer, packed, err := sandbox.PackArgs(spec, args, firstExtraFD+len(l.files))
	if err != nil {
		l.Close()

		return nil, err
	}
	l.files = append(l.files, packed)

	if gated {
		outer, err = l.nest(outer)
		if err != nil {
			l.Close()

			return nil, err
		}
	}

	// After the nesting and not before it, because a gated launch is two
	// bwraps and the inner one is not what is executed here. Logged
	// before, this line showed a command with no --userns, no outer bwrap
	// and no info descriptor: the arguments of a sandbox that was never
	// started. It sent a reading of these logs after the arguments of the
	// wrong process for as long as it took to notice.
	//
	// The sandbox's own arguments are still worth having, so both are
	// here, named for which is which.
	slog.Debug("exec", "binary", files.BwrapBinary,
		"args", container.RedactEnvArgs(outer),
		"sandbox", container.RedactEnvArgs(args),
		"gated", gated)

	l.cmd = execabs.Command(files.BwrapBinary, outer...) //nolint:gosec // the arguments are built from the workload config.
	l.cmd.ExtraFiles = l.files
	// The launch returns while the workload keeps running, so stdin stays
	// closed rather than leaving a detached application reading the
	// terminal the shell has taken back. Its output is still worth
	// showing: a workload that fails to start says why there.
	l.cmd.Stdout = os.Stdout
	l.cmd.Stderr = os.Stderr

	return l, nil
}

// nest wraps the sandbox so its user namespace is a child of the session's,
// and asks bwrap to report the pid of the sandbox it creates.
//
// The nesting is what makes the veth permitted. A helper holding
// CAP_NET_ADMIN in the session's namespace holds it in every namespace
// below, so it can put one end of a veth in this sandbox and address it,
// while the sandbox itself holds nothing over its own network namespace.
//
// The descriptors already prepared pass through the outer bwrap unchanged,
// which is what lets the inner one go on naming the seccomp filter and the
// packed argument file by number. See session.Namespace.Enter, where that
// was measured rather than assumed.
func (l *Launcher) nest(args []string) ([]string, error) {
	sess := session.Current()
	if err := sess.Start(); err != nil {
		return nil, err
	}

	ns, err := sess.Open(firstExtraFD + len(l.files))
	if err != nil {
		return nil, err
	}
	l.files = append(l.files, ns.File())

	// The namespace this sandbox is about to be nested in, named so that
	// a launch that later cannot be wired can be told apart from one that
	// was nested somewhere else. It is the one fact that distinguishes
	// the two and it is not otherwise recoverable from the logs.
	if id, err := ns.ID(); err == nil {
		slog.Debug("[session] nesting the sandbox in the session user namespace", "session", id)
	}

	info, report, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create the sandbox info pipe: %w", err)
	}
	l.info = info
	l.files = append(l.files, report)

	// The info option goes on the inner bwrap, which is the one that
	// creates the sandbox a workload runs in. The outer one only holds the
	// namespace open.
	return ns.Enter(sandbox.InfoFDArgs(args, firstExtraFD+len(l.files)-1)), nil
}

// Cmd is the process that was started. For a nested launch it is the outer
// bwrap and not the sandbox itself, which is what ChildPID is for. It is
// exposed because the caller records its pid and waits on it.
func (l *Launcher) Cmd() *execabs.Cmd {
	return l.cmd
}

// ForTest returns a Launcher standing for a sandbox that is already
// running, so that the ordering around a launch can be driven without
// starting one. pid is what ChildPID reports.
func ForTest(pid int) *Launcher {
	return &Launcher{pid: pid}
}

// Start runs the prepared sandbox.
func (l *Launcher) Start() error {
	if err := l.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start workload sandbox: %w", err)
	}

	return nil
}

// ChildPID returns the sandbox's own init process, in the host's pid
// namespace, as bwrap reported it.
//
// It is taken from bwrap rather than guessed from the process tree. A
// nested launch has two bwrap processes and a sandbox init below it, and
// which pid a veth has to be put next to is not something to infer from
// parentage.
func (l *Launcher) ChildPID() (int, error) {
	// bwrap reports it once, and the launch asks for it more than once: the
	// wiring needs it and so does the kill on the way out.
	if l.pid > 0 {
		return l.pid, nil
	}

	if l.info == nil {
		return 0, errors.New("bwrap: this sandbox was not asked to report its pid")
	}

	pid, err := sandbox.ChildPID(l.info, sandbox.InfoGrace)
	if err != nil {
		return 0, err
	}
	l.pid = pid

	return l.pid, nil
}

// Close releases the descriptors the launch prepared.
//
// The sandbox has its own copies once it has started, and the write end of
// the info pipe in particular has to be closed here: while this process
// holds one, a read on the other end would wait for a sandbox that has
// already gone rather than seeing the end of the file.
func (l *Launcher) Close() {
	for _, f := range l.files {
		f.Close()
	}
	l.files = nil

	if l.info != nil {
		l.info.Close()
		l.info = nil
	}
}

// Stop takes down a sandbox that must not be left running, and returns why.
//
// The process that was started is the outer bwrap for a nested launch, and
// killing it does not signal the sandbox below it. The sandbox's own init
// is pid 1 of its pid namespace, and a SIGKILL from an ancestor namespace
// takes the whole namespace with it, so that is the one to name when it is
// known.
func (l *Launcher) Stop(cause error) error {
	if l.pid > 0 {
		if err := syscall.Kill(l.pid, syscall.SIGKILL); err != nil {
			slog.Warn("failed to kill the unusable workload sandbox", "pid", l.pid, "error", err)
		}
	}

	if l.cmd != nil && l.cmd.Process != nil {
		if err := l.cmd.Process.Kill(); err != nil {
			slog.Warn("failed to kill the unusable workload sandbox", "error", err)
		}
		_ = l.cmd.Wait()
	}

	return cause
}
