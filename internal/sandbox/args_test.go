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
		Rootfs:   "/store/unpacked/sha256-abc/rootfs",
		Hostname: "qubesome-work",
		UID:      1000,
		GID:      1000,
		Net:      NetNone,
		Seccomp:  true,
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
