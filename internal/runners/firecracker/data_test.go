package firecracker

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errMkfsFake is what a fake cmdRunner returns, so a test can tell the
// failure it arranged from any other.
var errMkfsFake = errors.New("mkfs failed")

func TestEnsureDataDiskCreatesAndFormatsAMissingFile(t *testing.T) {
	t.Parallel()

	// The directory is not created by the test, because making it is part
	// of what is being checked.
	path := filepath.Join(t.TempDir(), "disks", "data.ext4")

	var (
		gotBin  string
		gotArgs []string
	)

	d := dataDisk{
		Path:    path,
		SizeMiB: 64,
		cmdRunner: func(bin string, args []string) error {
			gotBin = bin
			gotArgs = args
			return nil
		},
	}

	require.NoError(t, d.ensure())

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(64*mib), fi.Size())

	assert.Equal(t, files.MkfsExt4Binary, gotBin)
	assert.Equal(t, mkfsDataArgs(path), gotArgs)
}

// The disk is the only durable thing a machine has, so a file that is
// already there is used exactly as it is found.
func TestEnsureDataDiskLeavesAnExistingFileAlone(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data.ext4")
	require.NoError(t, os.WriteFile(path, []byte("a filesystem with things in it"), files.FileMode))

	d := dataDisk{
		Path:    path,
		SizeMiB: 64,
		cmdRunner: func(string, []string) error {
			t.Error("an existing data disk must never be formatted")
			return nil
		},
	}

	require.NoError(t, d.ensure())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "a filesystem with things in it", string(data))
}

func TestEnsureDataDiskWarnsWhenTheSizeNoLongerMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.ext4")
	require.NoError(t, createSparse(path, 32*mib))

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	d := dataDisk{Path: path, SizeMiB: 128, cmdRunner: func(string, []string) error { return nil }}
	require.NoError(t, d.ensure())

	out := buf.String()
	assert.Contains(t, out, "configuredMiB=128")
	assert.Contains(t, out, "actualMiB=32")

	// The warning is the whole of the response. Resizing is what it exists
	// instead of, because a shrink loses whatever was past the new end.
	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, int64(32*mib), fi.Size())
}

func TestEnsureDataDiskIsQuietWhenTheSizeMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.ext4")
	require.NoError(t, createSparse(path, 64*mib))

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	d := dataDisk{Path: path, SizeMiB: 64, cmdRunner: func(string, []string) error { return nil }}
	require.NoError(t, d.ensure())

	assert.Empty(t, buf.String())
}

// A file that was created and not formatted would be taken for a disk
// that already exists on the next launch, and never formatted at all.
func TestEnsureDataDiskRemovesAFileItCouldNotFormat(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data.ext4")

	d := dataDisk{
		Path:      path,
		SizeMiB:   64,
		cmdRunner: func(string, []string) error { return errMkfsFake },
	}

	require.ErrorIs(t, d.ensure(), errMkfsFake)

	_, err := os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestEnsureDataDiskRefusesADirectory(t *testing.T) {
	t.Parallel()

	d := dataDisk{Path: t.TempDir(), SizeMiB: 64}
	require.Error(t, d.ensure())
}

// The configured path goes through the same expansion every other host
// path in a workload does. The expansion table is process wide, which is
// why this one does not run in parallel.
func TestEnsureDataDiskExpandsTheConfiguredPath(t *testing.T) {
	dir := t.TempDir()

	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.NoError(t, env.Update("HOME", dir))
	t.Cleanup(func() { _ = env.Update("HOME", home) })

	path := filepath.Join(dir, "data.ext4")
	require.NoError(t, createSparse(path, 64*mib))

	got, err := EnsureDataDisk(types.MicroVMData{Path: "${HOME}/data.ext4", SizeMiB: 64})
	require.NoError(t, err)
	assert.Equal(t, path, got)
}

// The rootfs turns the journal off because it is discarded on shutdown.
// This disk is the one thing that is not, and the journal is what keeps
// it mountable after a host that lost power.
func TestDataDiskKeepsItsJournal(t *testing.T) {
	t.Parallel()

	args := mkfsDataArgs("/tmp/data.ext4")

	assert.NotContains(t, args, "^has_journal")
	assert.Equal(t, []string{
		"-q", "-F",
		"-m", "0",
		"-E", "lazy_itable_init=1,lazy_journal_init=1",
		"/tmp/data.ext4",
	}, args)
}
