package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// InfoGrace bounds the wait for bwrap to report a sandbox's pid.
//
// bwrap writes it as soon as it has cloned the sandbox, before anything from
// the image runs, so this is not sized for the work behind a launch. It is a
// ceiling for a sandbox that fails before it reports anything, which would
// otherwise leave the read waiting on a descriptor bwrap holds open for the
// whole life of the sandbox.
const InfoGrace = 30 * time.Second

// info is the object bwrap writes to its --info-fd. It carries more than
// this and everything else is ignored.
type info struct {
	// ChildPID is the sandbox's init process in the pid namespace bwrap was
	// started in. Nothing qubesome starts a sandbox from unshares one, so it
	// is the host's, which is what makes /proc/<pid>/ns/net nameable from
	// outside.
	ChildPID int `json:"child-pid"` //nolint:tagliatelle // bwrap chose the name and this end only reads it.
}

// InfoFDArgs prefixes bwrap options with the descriptor the sandbox pid is
// to be reported on.
//
// It is put in front of what Args rendered rather than added to Spec,
// because it describes a launch and not the sandbox. The position is
// deliberate: PackArgs finds the command by counting back from the end of
// the list, so options added in front of it change nothing about where that
// split falls.
func InfoFDArgs(args []string, fd int) []string {
	return append([]string{"--info-fd", strconv.Itoa(fd)}, args...)
}

// ChildPID reads a sandbox's pid from the descriptor bwrap reports it on.
//
// The pid is taken from bwrap rather than guessed from the process tree. A
// sandbox nested in the session's user namespace has two bwrap processes and
// a sandbox init between the launch and the workload, and which pid a veth
// has to be put next to is not something to infer from parentage.
//
// The read ends at the end of the JSON object rather than at the end of the
// file, which matters because bwrap keeps its copy of the write end for as
// long as the sandbox runs and no end of file arrives while it is up.
func ChildPID(r *os.File, grace time.Duration) (int, error) {
	if err := r.SetReadDeadline(time.Now().Add(grace)); err != nil {
		return 0, fmt.Errorf("failed to bound the wait for the sandbox pid: %w", err)
	}

	var i info
	if err := json.NewDecoder(r).Decode(&i); err != nil {
		return 0, fmt.Errorf("failed to read the sandbox pid: %w", err)
	}

	if i.ChildPID <= 0 {
		return 0, errors.New("bwrap reported no pid for the sandbox")
	}

	return i.ChildPID, nil
}

// NetnsPath names a process's network namespace.
//
// The pid is in the host's pid namespace, which is the only namespace every
// caller of this shares, and the one bwrap reports on its info descriptor.
func NetnsPath(pid int) string {
	return "/proc/" + strconv.Itoa(pid) + "/ns/net"
}

// RootPath names a process's root directory from outside its mount
// namespace.
//
// It resolves inside that namespace, absolute symlinks included, so a file
// can be left in a sandbox without entering it.
func RootPath(pid int) string {
	return "/proc/" + strconv.Itoa(pid) + "/root"
}
