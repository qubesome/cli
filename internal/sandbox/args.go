package sandbox

import (
	"errors"
	"fmt"
	"math"
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
// --dev-bind preceding --dev. The overlay on / comes first for the same
// reason, and every other mount lands on top of it.
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

	capHint := 37
	addCap := func(v int) error {
		if v < 0 || capHint > math.MaxInt-v {
			return errors.New("sandbox: argument list too large")
		}
		capHint += v
		return nil
	}
	addMulCap := func(multiplier, v int) error {
		if multiplier < 0 || v < 0 {
			return errors.New("sandbox: argument list too large")
		}
		if v != 0 && multiplier > math.MaxInt/v {
			return errors.New("sandbox: argument list too large")
		}
		return addCap(multiplier * v)
	}

	if err := addMulCap(3, len(s.Devices)); err != nil {
		return nil, err
	}
	if err := addMulCap(3, len(s.Mounts)); err != nil {
		return nil, err
	}
	if err := addMulCap(3, len(s.Env)); err != nil {
		return nil, err
	}
	if err := addCap(len(s.Args)); err != nil {
		return nil, err
	}

	args := make([]string, 0, capHint)
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

		// libdrm identifies a DRM device by reading sysfs, so with no
		// /sys Mesa enumerates no device at all, EGL finds no rendering
		// device and the compositor drops to llvmpipe. Verified against
		// bubblewrap 0.11.2 on an amdgpu host: with /sys absent
		// drmGetDevices2 reports zero devices and drmGetDevice2 fails
		// with EINVAL, and with this bind both succeed.
		//
		// The whole tree is shared because /sys/dev/char/226:128 and its
		// neighbours are symlinks into /sys/devices, so a subtree would
		// not resolve. Read-only is what container runners mount by
		// default, and a missing /sys is fatal rather than a silent drop
		// to software rendering, which is the failure this exists to
		// remove.
		"--ro-bind", "/sys", "/sys",

		// /tmp holds nothing from the image that the sandbox needs, and a
		// tmpfs bwrap mounts is owned by the sandbox user whatever mode
		// the image gave /tmp, so the compositor can always create its
		// runtime directory there.
		//
		// There is deliberately no equivalent on /run. The root is
		// already an overlay whose writes go to a tmpfs and are
		// discarded with the sandbox, so a second tmpfs would add
		// nothing and would hide whatever the image ships under /run,
		// /run/user/1000 included.
		"--tmpfs", "/tmp",
	)

	// The XDG runtime directory has to exist before anything inside looks
	// for it, and an image is not obliged to ship one. bwrap creates it
	// while it still holds the privileges of the sandbox setup, so the
	// mode of the image's /run does not matter, and 0700 owned by the
	// sandbox user is what the specification requires of it. A --bind on
	// the same path is emitted later and still wins, so a caller with a
	// host directory to put there is unaffected.
	if s.RuntimeDir != "" {
		args = append(args, "--perms", "0700", "--dir", s.RuntimeDir)
	}

	if s.Net == NetNone {
		args = append(args, "--unshare-net")
	}

	// --disable-userns takes no argument and bwrap rejects it without
	// --unshare-user, which is always passed above. bubblewrap 0.11.2
	// implements it by setting user.max_user_namespaces to 1 and then
	// spending that one on a second level namespace of its own, so
	// nothing inside has any budget left. It then unshares once more to
	// prove the block took, which is what --assert-userns-disabled would
	// check on its own, so pairing the two adds nothing.
	if s.DisableUserns {
		args = append(args, "--disable-userns")
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
