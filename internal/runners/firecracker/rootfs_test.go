package firecracker

import (
	"bytes"
	"encoding/json"
	"flag"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "update the golden files")

// The build itself cannot run here. The development container has no
// e2fsprogs, and an overlay inside a container fails with EINVAL, so what
// is checked is the command that would be run and not a filesystem read
// back. The header of hack/verify-microvm-rootfs.sh carries the host run
// that checked the other half.
//
// golden compares args against testdata/<name>.golden, one argument per
// line. Regenerate with: go test ./internal/runners/firecracker/... -update
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

func TestRootfsArgsBare(t *testing.T) {
	t.Parallel()

	args := rootfsArgs(rootfsBuild{
		Rootfs:      "/home/coder/.qubesome/images/unpacked/sha256-abc/rootfs",
		Target:      "/run/user/1000/qubesome/vm/dev-personal/rootfs.ext4",
		SizeMiB:     4096,
		QubesomeBin: "/usr/local/bin/qubesome",
		InitConfig:  "/run/user/1000/qubesome/vm/dev-personal/init.json",
	})

	golden(t, "rootfs-bare", args)
}

// The third path lands in /root, which debian:trixie has but a scratch
// image does not. bwrap creates the parents the image is missing, so a
// mapping is composed wherever the workload asked for it.
func TestRootfsArgsPaths(t *testing.T) {
	t.Parallel()

	args := rootfsArgs(rootfsBuild{
		Rootfs:  "/home/coder/.qubesome/images/unpacked/sha256-abc/rootfs",
		Target:  "/run/user/1000/qubesome/vm/dev-personal/rootfs.ext4",
		SizeMiB: 8192,
		Paths: []roPath{
			{Src: "/home/coder/git/dotfiles/.gitconfig", Dst: "/root/.gitconfig"},
			{Src: "/home/coder/git/dotfiles/.zshrc", Dst: "/root/.zshrc"},
			{Src: "/home/coder/git/dotfiles/ssh", Dst: "/root/.ssh"},
		},
		QubesomeBin: "/usr/local/bin/qubesome",
		InitConfig:  "/run/user/1000/qubesome/vm/dev-personal/init.json",
	})

	golden(t, "rootfs-paths", args)
}

// The golden files record the order, and a regeneration would take a new
// one without complaint, so the two constraints that silently write a
// wrong tree are asserted rather than only captured.
func TestRootfsArgsOrdering(t *testing.T) {
	t.Parallel()

	args := rootfsArgs(rootfsBuild{
		Rootfs:      "/store/rootfs",
		Target:      "/vm/rootfs.ext4",
		SizeMiB:     4096,
		Paths:       []roPath{{Src: "/host/gitconfig", Dst: "/root/.gitconfig"}},
		QubesomeBin: "/usr/local/bin/qubesome",
		InitConfig:  "/vm/init.json",
	})

	src := slices.Index(args, "--overlay-src")
	tmp := slices.Index(args, "--tmp-overlay")
	require.NotEqual(t, -1, src)
	require.NotEqual(t, -1, tmp)

	// bwrap consumes the pending layer when the overlay is mounted, so
	// anything between the two would be read as part of the same overlay.
	assert.Equal(t, src+2, tmp, "--overlay-src must be immediately before --tmp-overlay")

	// A mount hides what was put underneath it earlier, so a bind into the
	// composed tree emitted before the overlay is discarded in silence.
	for i, a := range args {
		if strings.HasPrefix(a, composedRoot+"/") {
			assert.Greater(t, i, tmp, "%q is bound before the overlay it lands in", a)
		}
	}

	// The command being wrapped starts after the separator, and mkfs is
	// pointed at the composed tree rather than at the bundle.
	sep := slices.Index(args, separator)
	require.NotEqual(t, -1, sep)
	assert.Equal(t, "/usr/sbin/mkfs.ext4", args[sep+1])
	assert.Equal(t, []string{"-d", composedRoot, "/vm/rootfs.ext4", "4096M"}, args[len(args)-4:])
}

func TestInitConfig(t *testing.T) {
	t.Parallel()

	cfg := initConfig(images.Bundle{
		Env: []string{"PATH=/usr/bin", "LANG=C.UTF-8"},
		Cwd: "/root",
	}, types.EffectiveWorkload{
		Name:    "dev-personal",
		Profile: &types.Profile{Name: "personal"},
		Workload: types.Workload{
			Command: "/bin/bash",
			Args:    []string{"-l"},
		},
	})

	assert.Equal(t, []string{"/bin/bash", "-l"}, cfg.Argv)
	assert.Equal(t, []string{"PATH=/usr/bin", "LANG=C.UTF-8", "QUBESOME_PROFILE=personal"}, cfg.Env)
	assert.Equal(t, "/root", cfg.Cwd)
	assert.Equal(t, "dev-personal", cfg.Hostname)
}

// A machine exists for the consoles that attach to it, and those bring
// their own argv, so an empty command is not an error here.
func TestInitConfigWithoutACommand(t *testing.T) {
	t.Parallel()

	cfg := initConfig(images.Bundle{}, types.EffectiveWorkload{Name: "dev-personal"})

	assert.Empty(t, cfg.Argv)
	assert.Empty(t, cfg.Env)
}

func TestWriteInitConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), initConfigFile)

	require.NoError(t, writeInitConfig(path, images.Bundle{Cwd: "/root"}, types.EffectiveWorkload{
		Name:     "dev-personal",
		Workload: types.Workload{Command: "/bin/sh"},
	}))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var got InitConfig
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, []string{"/bin/sh"}, got.Argv)
	assert.Equal(t, "/root", got.Cwd)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

// The image is created at its full size and holds nothing, so it costs
// what the guest writes into it and not what it was sized at.
func TestCreateSparse(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "rootfs.ext4")
	require.NoError(t, createSparse(path, 64*mib))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(64*mib), fi.Size())
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	// A rebuild reuses the path, and the previous image must not be read
	// back as the start of the new one.
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))
	require.NoError(t, createSparse(path, 32*mib))

	fi, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(32*mib), fi.Size())
}

// env.Expand reads a package level mapping rather than the process
// environment, so this cannot run in parallel.
func TestReadOnlyPaths(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "gitconfig")
	require.NoError(t, os.WriteFile(src, []byte("[user]\n"), 0o600))

	env.Add("QUBESOME_TEST_DOTFILES", dir)

	got := readOnlyPaths([]string{
		"${QUBESOME_TEST_DOTFILES}/gitconfig:/root/.gitconfig:ro",
		filepath.Join(dir, "missing") + ":/root/.zshrc:ro",
		src + ":/root/.rw",
	})

	assert.Equal(t, []roPath{{Src: src, Dst: "/root/.gitconfig"}}, got)
}

func TestWarnImageUser(t *testing.T) {
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	warnImageUser("docker.io/library/debian:trixie", 0)
	require.Empty(t, buf.String())

	warnImageUser("docker.io/library/nginx:latest", 101)

	out := buf.String()
	assert.Contains(t, out, "docker.io/library/nginx:latest")
	assert.Contains(t, out, "uid=101")
	assert.Contains(t, out, "root")
}
