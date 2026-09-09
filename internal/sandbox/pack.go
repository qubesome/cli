package sandbox

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// PackArgs moves the options in args into an anonymous file for bwrap's
// --args and returns the command line that references it.
//
// args is what Args rendered for s, and fd is the descriptor number the
// file will occupy in the child. os/exec numbers exec.Cmd.ExtraFiles from
// 3 upwards, so fd is 3 plus the index the returned file takes in it.
//
// A command line is world readable through /proc/<pid>/cmdline, and the
// profile passes its mTLS private key to the sandbox through --setenv.
// Container runners never had this problem, because the values travelled
// in the runner's environment rather than on a command line. Moving the
// options into a descriptor restores that.
//
// The command itself stays on the command line. bwrap reads "--" inside an
// --args descriptor as the end of that descriptor and drops what follows,
// so a command packed with the options never runs. It carries no secrets,
// and neither did the container runner's.
//
// The split is taken from the length of Spec.Args rather than by searching
// for "--", because a mount path or an environment value is allowed to be
// "--" and a search would cut the list in the wrong place.
//
// A memfd is used rather than a temp file so the arguments never touch the
// filesystem, where another process could read or replace them between
// writing and exec. It is created with MFD_CLOEXEC, which does not stop
// os/exec passing it on: each ExtraFiles entry is duped onto a low
// descriptor in the child without that flag.
func PackArgs(s Spec, args []string, fd int) ([]string, *os.File, error) {
	if fd < 3 {
		return nil, nil, fmt.Errorf("sandbox: args descriptor %d collides with the standard streams", fd)
	}

	i := len(args) - len(s.Args) - 1
	if i < 0 || args[i] != separator {
		return nil, nil, errors.New("sandbox: argument list does not end with the command")
	}

	opts, cmd := args[:i], args[i+1:]

	// bwrap reads the descriptor as a sequence of NUL terminated strings,
	// so every option carries its own terminator, the last one included.
	n := 0
	for _, a := range opts {
		n += len(a) + 1
	}

	b := make([]byte, 0, n)
	for _, a := range opts {
		b = append(b, a...)
		b = append(b, 0)
	}

	mfd, err := unix.MemfdCreate("qubesome-bwrap-args", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create memfd for sandbox arguments: %w", err)
	}
	f := os.NewFile(uintptr(mfd), "qubesome-bwrap-args")

	if _, err := f.Write(b); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("failed to write sandbox arguments: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("failed to rewind sandbox arguments: %w", err)
	}

	outer := make([]string, 0, 3+len(cmd))
	outer = append(outer, "--args", strconv.Itoa(fd), separator)

	return append(outer, cmd...), f, nil
}
