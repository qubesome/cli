package bwrap

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"syscall"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/runners/util/container"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/seccomp"
	"github.com/qubesome/cli/internal/session"
	"golang.org/x/sys/execabs"
)

// launcher is one prepared workload sandbox, before and after it starts.
//
// A workload taking a gateway address is started differently from one that
// is not, and the difference is not only the arguments. It nests inside the
// session's user namespace, so there is an outer bwrap between this process
// and the sandbox, and it reports its own pid on an info descriptor so the
// veth has somewhere to go. This holds both shapes, so the launch reads the
// same either way and only the failure handling has to know which it is.
type launcher struct {
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

// launch prepares the sandbox described by spec.
//
// gated says the sandbox is to take a gateway address, which is what
// decides both of the differences above.
func launch(spec sandbox.Spec, gated bool) (*launcher, error) {
	l := &launcher{}

	seccompFD := -1

	if spec.Seccomp {
		filter, err := seccomp.MemFD()
		if err != nil {
			l.close()

			return nil, err
		}

		seccompFD = firstExtraFD + len(l.files)
		l.files = append(l.files, filter)
	}

	args, err := sandbox.Args(spec, seccompFD)
	if err != nil {
		l.close()

		return nil, err
	}

	slog.Debug("exec", "binary", files.BwrapBinary, "args", container.RedactEnvArgs(args))

	// A mime enabled workload carries the profile's mTLS private key in
	// its environment, and a command line is world readable through
	// /proc. Only the descriptor holding the options, and the command,
	// stay on it.
	outer, packed, err := sandbox.PackArgs(spec, args, firstExtraFD+len(l.files))
	if err != nil {
		l.close()

		return nil, err
	}
	l.files = append(l.files, packed)

	if gated {
		outer, err = l.nest(outer)
		if err != nil {
			l.close()

			return nil, err
		}
	}

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
func (l *launcher) nest(args []string) ([]string, error) {
	sess := session.Current()
	if err := sess.Start(); err != nil {
		return nil, err
	}

	ns, err := sess.Open(firstExtraFD + len(l.files))
	if err != nil {
		return nil, err
	}
	l.files = append(l.files, ns.File())

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

// start runs the prepared sandbox.
func (l *launcher) start() error {
	if err := l.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start workload sandbox: %w", err)
	}

	return nil
}

// childPID returns the sandbox's own init process, in the host's pid
// namespace, as bwrap reported it.
//
// It is taken from bwrap rather than guessed from the process tree. A
// nested launch has two bwrap processes and a sandbox init below it, and
// which pid a veth has to be put next to is not something to infer from
// parentage.
func (l *launcher) childPID() (int, error) {
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

// close releases the descriptors the launch prepared.
//
// The sandbox has its own copies once it has started, and the write end of
// the info pipe in particular has to be closed here: while this process
// holds one, a read on the other end would wait for a sandbox that has
// already gone rather than seeing the end of the file.
func (l *launcher) close() {
	for _, f := range l.files {
		f.Close()
	}
	l.files = nil

	if l.info != nil {
		l.info.Close()
		l.info = nil
	}
}

// stop takes down a sandbox that must not be left running, and returns why.
//
// The process that was started is the outer bwrap for a nested launch, and
// killing it does not signal the sandbox below it. The sandbox's own init
// is pid 1 of its pid namespace, and a SIGKILL from an ancestor namespace
// takes the whole namespace with it, so that is the one to name when it is
// known.
func (l *launcher) stop(cause error) error {
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
