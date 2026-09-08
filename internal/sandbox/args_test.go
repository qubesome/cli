package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// golden compares args against testdata/<name>.golden, one argument per
// line. Regenerate with: go test ./internal/sandbox/... -update
func golden(t *testing.T, name string, args []string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	got := strings.Join(args, "\n") + "\n"

	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to create the golden file")
	assert.Equal(t, string(want), got)
}

func TestArgsMinimal(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs: "/store/unpacked/sha256-abc/rootfs",
		UID:    1000,
		GID:    1000,
		Args:   []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	golden(t, "minimal", args)
}

func TestArgsProfile(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs:     "/store/unpacked/sha256-abc/rootfs",
		Hostname:   "qubesome-work",
		UID:        1000,
		GID:        1000,
		Net:        NetNone,
		Seccomp:    true,
		RuntimeDir: "/run/user/1000",

		DieWithParent: true,
		DisableUserns: true,
		Env: []string{
			"PATH=/usr/local/bin:/usr/bin:/bin",
			"DISPLAY=:21",
		},
		Devices: []string{"/dev/dri/renderD128", "/dev/dri/card0"},
		Mounts: []Mount{
			{Src: "/etc/localtime", Dst: "/etc/localtime", ReadOnly: true},
			{Src: "/tmp/.X11-unix", Dst: "/tmp/.X11-unix"},
			{Src: "/run/user/1000/qubesome/work/qube.sock", Dst: "/tmp/qube.sock", ReadOnly: true},
			{Src: "/home/levi/git", Dst: "/data/git"},
		},
		Args: []string{"/usr/local/bin/qubesome", "profile-display"},
		Cwd:  "/home/xorg-user",
	}, 7)
	require.NoError(t, err)

	golden(t, "profile", args)
}

// Nested user namespaces are how a sandboxed process regains the
// capabilities needed to reach the mount syscalls the vendored seccomp
// profile allows, so the flag is the profile's answer to that. It stays off
// by default for workloads that nest a sandbox of their own.
func TestArgsDisableUserns(t *testing.T) {
	t.Parallel()

	on, err := Args(Spec{
		Rootfs:        "/rootfs",
		Args:          []string{"/bin/sh"},
		DisableUserns: true,
	}, -1)
	require.NoError(t, err)
	assert.Contains(t, on, "--disable-userns")

	// bwrap refuses --disable-userns without --unshare-user.
	assert.Contains(t, on, "--unshare-user")

	off, err := Args(Spec{
		Rootfs: "/rootfs",
		Args:   []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)
	assert.NotContains(t, off, "--disable-userns")
}

// The flag is a bwrap option rather than a seccomp rule, so a profile that
// asks for no filter still gets it.
func TestArgsDisableUsernsIsIndependentOfSeccomp(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs:        "/rootfs",
		Args:          []string{"/bin/sh"},
		DisableUserns: true,
		Seccomp:       false,
	}, -1)
	require.NoError(t, err)

	assert.Contains(t, args, "--disable-userns")
	assert.NotContains(t, args, "--seccomp")
}

// A profile ties its sandbox to the process that serves it. A workload
// does not, and it is the workload that would be lost: the launch returns
// as soon as it is up, so a sandbox dying with its launcher would never
// outlive a qubesome run at a terminal.
func TestArgsDieWithParent(t *testing.T) {
	t.Parallel()

	on, err := Args(Spec{
		Rootfs:        "/rootfs",
		Args:          []string{"/bin/sh"},
		DieWithParent: true,
	}, -1)
	require.NoError(t, err)
	assert.Contains(t, on, "--die-with-parent")

	off, err := Args(Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}, -1)
	require.NoError(t, err)
	assert.NotContains(t, off, "--die-with-parent")
}

// bwrap has no default of its own to fall back on here: with no --chdir
// it starts in /, which is what a spec with no working directory wants.
func TestArgsOmitsAnEmptyCwd(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}, -1)
	require.NoError(t, err)

	assert.NotContains(t, args, "--chdir")
}

// /tmp is a tmpfs, and the X11 socket dir and qube.sock are bound inside
// it. A bind that precedes the tmpfs it lands in is silently discarded, so
// the order is a correctness property rather than a formatting one.
func TestArgsTmpfsPrecedesItsBinds(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs: "/rootfs",
		Mounts: []Mount{{Src: "/tmp/.X11-unix", Dst: "/tmp/.X11-unix"}},
		Args:   []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	assert.Less(t, indexOfArg(args, "--tmpfs", "/tmp"),
		indexOfArg(args, "--bind", "/tmp/.X11-unix"))
}

// The root is already an overlay whose writes are discarded with the
// sandbox, so a tmpfs on /run would only hide what the image ships there,
// /run/user/1000 included.
func TestArgsLeavesRunToTheImage(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}, -1)
	require.NoError(t, err)

	assert.Equal(t, -1, indexOfArg(args, "--tmpfs", "/run"))
}

// XDG_RUNTIME_DIR has to exist whatever the image ships, and the
// specification requires it to be private to its owner.
func TestArgsCreatesTheRuntimeDir(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs:     "/rootfs",
		RuntimeDir: "/run/user/1000",
		Args:       []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	i := indexOfArg(args, "--dir", "/run/user/1000")
	require.NotEqual(t, -1, i)
	require.Greater(t, i, 1)
	assert.Equal(t, []string{"--perms", "0700"}, args[i-2:i])
}

// A caller with a host directory for the runtime dir binds it as a mount,
// and that bind is emitted later so it lands on top of the created one.
func TestArgsRuntimeDirPrecedesABindOnTheSamePath(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs:     "/rootfs",
		RuntimeDir: "/run/user/1000",
		Mounts:     []Mount{{Src: "/host/user", Dst: "/run/user/1000"}},
		Args:       []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	assert.Less(t, indexOfArg(args, "--dir", "/run/user/1000"),
		indexOfArg(args, "--bind", "/host/user"))
}

func TestArgsOmitsAnEmptyRuntimeDir(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}, -1)
	require.NoError(t, err)

	assert.NotContains(t, args, "--dir")
	assert.NotContains(t, args, "--perms")
}

// Mesa reads sysfs to enumerate DRM devices, so a sandbox without /sys has
// no GPU and falls back to software rendering. It is shared read-only, the
// way container runners share it.
func TestArgsBindsSysReadOnly(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}, -1)
	require.NoError(t, err)

	assert.NotEqual(t, -1, indexOfArg(args, "--ro-bind", "/sys"))
	assert.Equal(t, -1, indexOfArg(args, "--bind", "/sys"))
	assert.Equal(t, -1, indexOfArg(args, "--dev-bind", "/sys"))
}

// The overlay on / hides anything mounted under it earlier, and a bind
// landing inside /sys would be hidden by a later bind of /sys itself. The
// whole tree is shared in one go because /sys/dev/char entries are
// symlinks into /sys/devices, which a subtree bind would not resolve.
func TestArgsSysSitsBetweenTheRootAndItsMounts(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs: "/rootfs",
		Mounts: []Mount{{Src: "/sys/class/backlight", Dst: "/sys/class/backlight"}},
		Args:   []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	assert.Less(t, indexOfArg(args, "--tmp-overlay", "/"),
		indexOfArg(args, "--ro-bind", "/sys"))
	assert.Less(t, indexOfArg(args, "--ro-bind", "/sys"),
		indexOfArg(args, "--bind", "/sys/class/backlight"))
}

// --dev mounts a fresh devtmpfs, which would hide any device bound before
// it.
func TestArgsDevPrecedesDeviceBinds(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs:  "/rootfs",
		Devices: []string{"/dev/dri/renderD128"},
		Args:    []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	assert.Less(t, indexOfArg(args, "--dev", "/dev"),
		indexOfArg(args, "--dev-bind", "/dev/dri/renderD128"))
}

func TestArgsSeccompRequiresFD(t *testing.T) {
	t.Parallel()

	_, err := Args(Spec{Rootfs: "/rootfs", Seccomp: true, Args: []string{"/bin/sh"}}, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "seccomp")
}

func TestArgsRejectsEmptyRootfs(t *testing.T) {
	t.Parallel()

	_, err := Args(Spec{Args: []string{"/bin/sh"}}, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rootfs")
}

func TestArgsRejectsEmptyArgs(t *testing.T) {
	t.Parallel()

	_, err := Args(Spec{Rootfs: "/rootfs"}, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command")
}

func TestArgsRejectsMalformedEnv(t *testing.T) {
	t.Parallel()

	_, err := Args(Spec{
		Rootfs: "/rootfs",
		Env:    []string{"NOT_AN_ASSIGNMENT"},
		Args:   []string{"/bin/sh"},
	}, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_AN_ASSIGNMENT")
}

func TestArgsKeepsEqualsInEnvValue(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs: "/rootfs",
		Env:    []string{"Q_MTLS_CA=a=b=c"},
		Args:   []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	i := indexOfArg(args, "--setenv", "Q_MTLS_CA")
	require.NotEqual(t, -1, i)
	assert.Equal(t, "a=b=c", args[i+2])
}

func indexOfArg(args []string, flag, value string) int {
	for i := range args {
		if args[i] == flag && i+1 < len(args) && args[i+1] == value {
			return i
		}
	}
	return -1
}

// bwrap applies capability arguments in the order they appear, so a
// --cap-add emitted before the --cap-drop ALL is undone by it with no
// diagnostic. The whole grant depends on this ordering.
func TestArgsCapAddFollowsTheDrop(t *testing.T) {
	t.Parallel()

	args, err := Args(Spec{
		Rootfs:  "/rootfs",
		CapsAdd: []string{"CAP_NET_ADMIN"},
		Args:    []string{"/bin/sh"},
	}, -1)
	require.NoError(t, err)

	assert.Less(t, indexOfArg(args, "--cap-drop", "ALL"),
		indexOfArg(args, "--cap-add", "CAP_NET_ADMIN"))
}

// bwrap rejects a bare NET_ADMIN at launch, which is late and only visible
// on the sandbox's stderr.
func TestArgsRejectsACapWithoutThePrefix(t *testing.T) {
	t.Parallel()

	_, err := Args(Spec{
		Rootfs:  "/rootfs",
		CapsAdd: []string{"NET_ADMIN"},
		Args:    []string{"/bin/sh"},
	}, -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CAP_")
}
