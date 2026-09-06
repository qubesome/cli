package sandbox

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// separator ends bwrap's own options and begins the command.
const separator = "--"

// Args renders a Spec into bwrap arguments.
//
// seccompFD is the descriptor number the filter will occupy in the child,
// or a negative number when Spec.Seccomp is false.
//
// Ordering carries meaning. A tmpfs or a fresh /dev hides anything mounted
// underneath it earlier, so both are emitted before the binds that land
// inside them. Verified against bubblewrap 0.11.2: a --bind that precedes
// the --tmpfs it lands in is silently discarded, and the same holds for a
// --dev-bind preceding --dev.
func Args(s Spec, seccompFD int) ([]string, error) {
	if s.Rootfs == "" {
		return nil, errors.New("sandbox: rootfs is required")
	}
	if len(s.Args) == 0 {
		return nil, errors.New("sandbox: command is required")
	}
	if s.Seccomp && seccompFD < 0 {
		return nil, errors.New("sandbox: seccomp is enabled but no filter descriptor was given")
	}

	args := make([]string, 0, 32+3*len(s.Devices)+3*len(s.Mounts)+3*len(s.Env)+len(s.Args))
	args = append(args,
		// The image is shared read-only and every write lands in a tmpfs
		// that goes away with the sandbox.
		"--overlay-src", s.Rootfs,
		"--tmp-overlay", "/",

		"--unshare-user",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--unshare-cgroup",

		"--uid", strconv.Itoa(s.UID),
		"--gid", strconv.Itoa(s.GID),

		"--cap-drop", "ALL",
		"--die-with-parent",

		// The sandbox environment is built from the image and the profile,
		// so the host environment must not leak into it.
		"--clearenv",

		"--proc", "/proc",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--tmpfs", "/run",
	)

	if s.Net == NetNone {
		args = append(args, "--unshare-net")
	}

	if s.Hostname != "" {
		args = append(args, "--hostname", s.Hostname)
	}

	// --chdir takes the directory as its single argument, and bwrap
	// applies it once the sandbox is set up, so the directory is resolved
	// inside the sandbox rather than on the host.
	if s.Cwd != "" {
		args = append(args, "--chdir", s.Cwd)
	}

	if !s.Interactive {
		args = append(args, "--new-session")
	}

	for _, d := range s.Devices {
		args = append(args, "--dev-bind", d, d)
	}

	for _, m := range s.Mounts {
		if m.Src == "" || m.Dst == "" {
			return nil, fmt.Errorf("sandbox: incomplete mount %+v", m)
		}
		flag := "--bind"
		if m.ReadOnly {
			flag = "--ro-bind"
		}
		args = append(args, flag, m.Src, m.Dst)
	}

	// --setenv takes the name and the value as separate arguments, so a
	// value containing an equals sign survives as long as only the first
	// one is treated as the separator.
	for _, e := range s.Env {
		name, value, ok := strings.Cut(e, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("sandbox: malformed environment entry %q", e)
		}
		args = append(args, "--setenv", name, value)
	}

	if s.Seccomp {
		args = append(args, "--seccomp", strconv.Itoa(seccompFD))
	}

	args = append(args, separator)

	return append(args, s.Args...), nil
}
