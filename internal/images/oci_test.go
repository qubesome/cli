package images

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errUnpackFake is a sentinel error a fake cmdRunner returns, so tests can
// assert it propagates unwrapped through errors.Is.
var errUnpackFake = errors.New("fake tool failure")

// newTestStore returns a Store whose OCI index is a copy of
// testdata/oci/index.json under a fresh root, so Digest resolves
// "ghcr.io/qubesome/xorg:latest" to fixtureDigest.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "oci"), 0o700))

	index, err := os.ReadFile("testdata/oci/index.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "oci", "index.json"), index, 0o600)) //nolint:gosec // index.json is a fixed fixture name under t.TempDir().

	return &Store{Root: root}
}

// newExistingBundle populates s's bundle dir for fixtureDigest with the
// fixture config and an empty rootfs, so Unpack takes the reuse path.
func newExistingBundle(t *testing.T, s *Store) string {
	t.Helper()

	dir, err := s.bundleDir(fixtureDigest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "rootfs"), 0o700))

	cfg, err := os.ReadFile("testdata/bundle/config.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0o600)) //nolint:gosec // config.json is a fixed fixture name under t.TempDir().

	return dir
}

func TestPullArgs(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "/store"}

	assert.Equal(t, []string{
		"copy",
		"docker://ghcr.io/qubesome/xorg:latest",
		"oci:/store/oci:latest",
	}, s.pullArgs("ghcr.io/qubesome/xorg:latest"))
}

func TestUnpackArgs(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "/store"}

	assert.Equal(t, []string{
		"unpack",
		"--rootless",
		"--image", "oci:/store/oci:latest",
		"/store/unpacked/tmp-123",
	}, s.unpackArgs("ghcr.io/qubesome/xorg:latest", "/store/unpacked/tmp-123"))
}

// Unpack must never leave a partially extracted bundle under its final
// name, because a later start would find it and use it.
func TestUnpackReusesExistingBundle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dir := newExistingBundle(t, s)

	// umoci is not installed in the test environment. Reaching it would
	// fail, so a pass proves the existing bundle was reused.
	b, err := s.Unpack("ghcr.io/qubesome/xorg:latest")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "rootfs"), b.Rootfs)
	assert.Equal(t, 1000, b.UID)
}

// TestUnpackReuseDoesNotExec proves the reuse path directly: with a real
// umoci installed, TestUnpackReusesExistingBundle above would still pass
// even if the reuse check were broken and it fell through to a working
// extraction. Failing loudly on any exec closes that gap.
func TestUnpackReuseDoesNotExec(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	newExistingBundle(t, s)

	s.cmdRunner = func(bin string, args []string) error {
		t.Fatalf("unexpected exec of %s %v: the bundle already exists and should have been reused", bin, args)
		return nil
	}

	_, err := s.Unpack("ghcr.io/qubesome/xorg:latest")
	require.NoError(t, err)
}

// TestUnpackBuildsUnderTempAndRenames exercises the temp-and-rename dance
// with a fake umoci, verifying the final bundle lands at the digest
// directory and no temp directory is left behind.
func TestUnpackBuildsUnderTempAndRenames(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	cfg, err := os.ReadFile("testdata/bundle/config.json")
	require.NoError(t, err)

	var gotBin string
	var gotArgs []string
	s.cmdRunner = func(bin string, args []string) error {
		gotBin = bin
		gotArgs = args

		// umoci's last argument is the bundle directory it creates
		// itself, so the fake must create it too.
		dest := args[len(args)-1]
		if err := os.MkdirAll(filepath.Join(dest, "rootfs"), 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, "config.json"), cfg, 0o600) //nolint:gosec // dest is the temp bundle dir Unpack itself created under t.TempDir().
	}

	b, err := s.Unpack("ghcr.io/qubesome/xorg:latest")
	require.NoError(t, err)

	assert.Equal(t, files.UmociBinary, gotBin)
	assert.Equal(t, "--rootless", gotArgs[1])

	dir, err := s.bundleDir(fixtureDigest)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "rootfs"), b.Rootfs)

	entries, err := os.ReadDir(filepath.Join(s.Root, "unpacked"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "tmp-"), "leftover temp dir: %s", e.Name())
	}
}

// TestUnpackPropagatesRunError confirms a failed extraction never renames
// a partial temp directory into place, and that the temp directory is
// still cleaned up.
func TestUnpackPropagatesRunError(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	s.cmdRunner = func(bin string, args []string) error {
		return errUnpackFake
	}

	_, err := s.Unpack("ghcr.io/qubesome/xorg:latest")
	require.ErrorIs(t, err, errUnpackFake)

	dir, err := s.bundleDir(fixtureDigest)
	require.NoError(t, err)
	_, statErr := os.Stat(dir)
	assert.True(t, os.IsNotExist(statErr), "bundle dir must not exist after a failed unpack")

	entries, err := os.ReadDir(filepath.Join(s.Root, "unpacked"))
	require.NoError(t, err)
	assert.Empty(t, entries, "temp dir must be cleaned up after a failed unpack")
}

func TestPullInvokesSkopeoAndCreatesLayout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := &Store{Root: root}

	var gotBin string
	var gotArgs []string
	s.cmdRunner = func(bin string, args []string) error {
		gotBin = bin
		gotArgs = args
		return nil
	}

	require.NoError(t, s.Pull("ghcr.io/qubesome/xorg:latest"))

	assert.Equal(t, files.SkopeoBinary, gotBin)
	assert.Equal(t, []string{
		"copy",
		"docker://ghcr.io/qubesome/xorg:latest",
		"oci:" + filepath.Join(root, "oci") + ":latest",
	}, gotArgs)

	_, err := os.Stat(filepath.Join(root, "oci"))
	require.NoError(t, err)
}

func TestPullPropagatesRunError(t *testing.T) {
	t.Parallel()

	s := &Store{Root: t.TempDir()}
	s.cmdRunner = func(bin string, args []string) error {
		return errUnpackFake
	}

	err := s.Pull("ghcr.io/qubesome/xorg:latest")
	require.ErrorIs(t, err, errUnpackFake)
}

// TestRunCmdAttachesStderr exercises runCmd against /bin/sh, which is
// always present, so the stderr-attaching error path is verified without
// either skopeo or umoci installed.
func TestRunCmdAttachesStderr(t *testing.T) {
	t.Parallel()

	err := runCmd(files.ShBinary, []string{"-c", "echo boom >&2; exit 1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestRunCmdWithoutStderr(t *testing.T) {
	t.Parallel()

	err := runCmd(files.ShBinary, []string{"-c", "exit 1"})
	require.Error(t, err)
	assert.Equal(t, `sh [-c exit 1]: exit status 1`, err.Error())
}

// TestPullAndUnpack exercises the real tools. It is gated because unit
// tests must stay hermetic and because CI may have neither binary.
//
// Run with: QUBESOME_TEST_OCI=1 go test ./internal/images/... -run TestPullAndUnpack
func TestPullAndUnpack(t *testing.T) {
	if os.Getenv("QUBESOME_TEST_OCI") == "" {
		t.Skip("set QUBESOME_TEST_OCI=1 to run against skopeo and umoci")
	}

	s := &Store{Root: t.TempDir()}

	const ref = "docker.io/library/alpine:3.20"
	require.NoError(t, s.Pull(ref))

	digest, err := s.Digest(ref)
	require.NoError(t, err)
	assert.Contains(t, digest, "sha256:")

	b, err := s.Unpack(ref)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(b.Rootfs, "bin", "sh"))
	assert.NoError(t, err, "expected the unpacked rootfs to contain /bin/sh")

	// A second unpack must reuse the bundle rather than extract again.
	again, err := s.Unpack(ref)
	require.NoError(t, err)
	assert.Equal(t, b.Rootfs, again.Rootfs)
}
