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

// newTestStore returns a Store whose layouts are a copy of testdata/oci
// under a fresh root, so Digest resolves xorgRef and kaliRef to their
// fixture digests.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	root := t.TempDir()

	layouts, err := os.ReadDir("testdata/oci")
	require.NoError(t, err)

	for _, l := range layouts {
		dst := filepath.Join(root, "oci", l.Name())
		require.NoError(t, os.MkdirAll(dst, 0o700))

		index, err := os.ReadFile(filepath.Join("testdata/oci", l.Name(), "index.json"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dst, "index.json"), index, 0o600)) //nolint:gosec // index.json is a fixed fixture name under t.TempDir().
	}

	return &Store{Root: root}
}

// newExistingBundle populates s's bundle dir for digest with the fixture
// config and an empty rootfs, so Unpack takes the reuse path.
func newExistingBundle(t *testing.T, s *Store, digest string) string {
	t.Helper()

	dir, err := s.bundleDir(digest)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "rootfs"), 0o700))

	cfg, err := os.ReadFile("testdata/bundle/config.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), cfg, 0o600)) //nolint:gosec // config.json is a fixed fixture name under t.TempDir().

	return dir
}

func TestPullArgs(t *testing.T) {
	t.Parallel()

	layout := "/store/oci/" + xorgKey

	assert.Equal(t, []string{
		"copy",
		"docker://ghcr.io/qubesome/xorg:latest",
		"oci:/store/oci/" + xorgKey + ":" + xorgKey,
	}, pullArgs(layout, xorgRef))
}

func TestUnpackArgs(t *testing.T) {
	t.Parallel()

	layout := "/store/oci/" + xorgKey

	// No oci: prefix. umoci takes a bare path[:tag], unlike skopeo above.
	assert.Equal(t, []string{
		"unpack",
		"--rootless",
		"--image", "/store/oci/" + xorgKey + ":" + xorgKey,
		"/store/unpacked/tmp-123",
	}, unpackArgs(layout, xorgRef, "/store/unpacked/tmp-123"))
}

// The two tools spell the same layout and key differently, and passing
// skopeo's form to umoci is what a real start failed on. Pinning the
// difference here so it cannot be folded back into one form unnoticed.
func TestSkopeoAndUmociImagesDiffer(t *testing.T) {
	t.Parallel()

	layout := "/store/oci/" + xorgKey

	assert.Equal(t, "oci:"+layout+":"+xorgKey, skopeoImage(layout, xorgRef))
	assert.Equal(t, layout+":"+xorgKey, umociImage(layout, xorgRef))
	assert.NotEqual(t, skopeoImage(layout, xorgRef), umociImage(layout, xorgRef))
	assert.NotContains(t, umociImage(layout, xorgRef), "oci:")

	// umoci parses --image with strings.Cut on the first colon, so the
	// form it is handed has to split into the layout and the key.
	dir, tag, ok := strings.Cut(umociImage(layout, xorgRef), ":")
	require.True(t, ok)
	assert.Equal(t, layout, dir)
	assert.Equal(t, xorgKey, tag)
}

// Cutting at the first colon means the layout path must not carry one.
// storeKey never emits a colon, but the store root comes from the home
// directory, so a colon there would have either tool work on the wrong
// directory under a nonsense tag.
func TestLayoutRejectsAColonInTheStorePath(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "/home/awkward:name/.qubesome/images"}
	s.cmdRunner = func(bin string, args []string) error {
		t.Fatalf("unexpected exec of %s %v: the store path is unusable", bin, args)
		return nil
	}

	_, err := s.layout(xorgRef)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains a colon")

	// Neither tool can be reached without a layout, so both report it.
	require.ErrorContains(t, s.Pull(xorgRef), "contains a colon")

	_, err = s.Unpack(xorgRef)
	require.ErrorContains(t, err, "contains a colon")
}

// Unpack must never leave a partially extracted bundle under its final
// name, because a later start would find it and use it.
func TestUnpackReusesExistingBundle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dir := newExistingBundle(t, s, xorgDigest)

	// umoci is not installed in the test environment. Reaching it would
	// fail, so a pass proves the existing bundle was reused.
	b, err := s.Unpack(xorgRef)
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
	newExistingBundle(t, s, xorgDigest)

	s.cmdRunner = func(bin string, args []string) error {
		t.Fatalf("unexpected exec of %s %v: the bundle already exists and should have been reused", bin, args)
		return nil
	}

	_, err := s.Unpack(xorgRef)
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

	b, err := s.Unpack(xorgRef)
	require.NoError(t, err)

	assert.Equal(t, files.UmociBinary, gotBin)
	assert.Equal(t, "--rootless", gotArgs[1])

	dir, err := s.bundleDir(xorgDigest)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "rootfs"), b.Rootfs)

	entries, err := os.ReadDir(filepath.Join(s.Root, "unpacked"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "tmp-"), "leftover temp dir: %s", e.Name())
	}
}

// TestUnpackReusesTheWinnersBundleWhenTheRenameLoses simulates two starts
// unpacking the same image at once. os.Rename refuses an existing
// directory, so whichever finishes second cannot move its bundle into
// place. It must reuse the bundle already there rather than fail the start.
func TestUnpackReusesTheWinnersBundleWhenTheRenameLoses(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	cfg, err := os.ReadFile("testdata/bundle/config.json")
	require.NoError(t, err)

	s.cmdRunner = func(bin string, args []string) error {
		dest := args[len(args)-1]
		if err := os.MkdirAll(filepath.Join(dest, "rootfs"), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, "config.json"), cfg, 0o600); err != nil { //nolint:gosec // dest is the temp bundle dir Unpack itself created under t.TempDir().
			return err
		}

		// The other start wins the race and claims the digest dir while
		// this one is still extracting.
		newExistingBundle(t, s, xorgDigest)
		return nil
	}

	b, err := s.Unpack(xorgRef)
	require.NoError(t, err)

	dir, err := s.bundleDir(xorgDigest)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "rootfs"), b.Rootfs)
	assert.Equal(t, 1000, b.UID)
}

// A rename failure with no usable bundle at the destination is still a
// failure. Only the concurrent unpack case is recovered from.
func TestUnpackFailsWhenTheDestinationHasNoBundle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	cfg, err := os.ReadFile("testdata/bundle/config.json")
	require.NoError(t, err)

	s.cmdRunner = func(bin string, args []string) error {
		dest := args[len(args)-1]
		if err := os.MkdirAll(filepath.Join(dest, "rootfs"), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dest, "config.json"), cfg, 0o600); err != nil { //nolint:gosec // dest is the temp bundle dir Unpack itself created under t.TempDir().
			return err
		}

		dir, dirErr := s.bundleDir(xorgDigest)
		if dirErr != nil {
			return dirErr
		}
		return os.MkdirAll(dir, 0o700)
	}

	_, err = s.Unpack(xorgRef)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to move bundle into place")
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

	_, err := s.Unpack(xorgRef)
	require.ErrorIs(t, err, errUnpackFake)

	dir, err := s.bundleDir(xorgDigest)
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

	require.NoError(t, s.Pull(xorgRef))

	layout := filepath.Join(root, "oci", xorgKey)

	assert.Equal(t, files.SkopeoBinary, gotBin)
	assert.Equal(t, []string{
		"copy",
		"docker://ghcr.io/qubesome/xorg:latest",
		"oci:" + layout + ":" + xorgKey,
	}, gotArgs)

	_, err := os.Stat(layout)
	require.NoError(t, err)
}

func TestPullPropagatesRunError(t *testing.T) {
	t.Parallel()

	s := &Store{Root: t.TempDir()}
	s.cmdRunner = func(bin string, args []string) error {
		return errUnpackFake
	}

	err := s.Pull(xorgRef)
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

func TestStoreResolve(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dir := newExistingBundle(t, s, xorgDigest)

	b, err := s.Resolve(xorgRef)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "rootfs"), b.Rootfs)
}

// Two images sharing a tag each resolve to their own bundle. Keying by
// tag alone had one layout entry serve both, so whichever was pulled last
// owned it.
func TestStoreResolveDoesNotCollideOnASharedTag(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	xorg := newExistingBundle(t, s, xorgDigest)
	kali := newExistingBundle(t, s, kaliDigest)
	require.NotEqual(t, xorg, kali)

	bx, err := s.Resolve(xorgRef)
	require.NoError(t, err)

	bk, err := s.Resolve(kaliRef)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(xorg, "rootfs"), bx.Rootfs)
	assert.Equal(t, filepath.Join(kali, "rootfs"), bk.Rootfs)
}

// A store holding only xorg must not report kali as present. This was the
// deterministic form of the collision: Resolve answered with xorg's
// bundle, the pull was skipped, and the profile ran the wrong root
// filesystem.
func TestStoreResolveRejectsAnotherImageWithTheSameTag(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	require.NoError(t, os.RemoveAll(filepath.Join(s.Root, "oci", kaliKey)))
	newExistingBundle(t, s, xorgDigest)

	_, err := s.Resolve(kaliRef)
	require.Error(t, err)
	assert.Contains(t, err.Error(), kaliKey)
}

func TestStoreResolveNotUnpacked(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	_, err := s.Resolve(xorgRef)
	require.Error(t, err)
}

func TestStoreResolveNotInTheStore(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	_, err := s.Resolve("ghcr.io/qubesome/xorg:missing")
	require.Error(t, err)
}

// An image that is already unpacked is used without reaching the
// registry, so a start works offline.
func TestPullImageSkipsAWarmStore(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	dir := newExistingBundle(t, s, xorgDigest)

	var ran []string
	s.cmdRunner = func(bin string, _ []string) error {
		ran = append(ran, bin)
		return nil
	}

	b, err := pullImage(s, xorgRef)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, "rootfs"), b.Rootfs)
	assert.Empty(t, ran, "a warm store must not shell out to skopeo or umoci")
}

func TestPullImagePullsAColdStore(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	var ran []string
	s.cmdRunner = func(bin string, _ []string) error {
		ran = append(ran, filepath.Base(bin))
		newExistingBundle(t, s, xorgDigest)
		return nil
	}

	_, err := pullImage(s, xorgRef)
	require.NoError(t, err)

	assert.Equal(t, []string{filepath.Base(files.SkopeoBinary)}, ran)
}

// PullAll backs qubesome images, whose whole purpose is to refresh, so it
// pulls even when the store is warm.
func TestRefreshImagePullsAWarmStore(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	newExistingBundle(t, s, xorgDigest)

	var ran []string
	s.cmdRunner = func(bin string, _ []string) error {
		ran = append(ran, filepath.Base(bin))
		return nil
	}

	_, err := refreshImage(s, xorgRef)
	require.NoError(t, err)

	assert.Equal(t, []string{filepath.Base(files.SkopeoBinary)}, ran)
}
