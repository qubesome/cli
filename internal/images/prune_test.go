package images

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	oldDigest   = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	olderDigest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
)

// setCurrent rewrites the index of ref's layout so that it names digest,
// which is what a pull that fetched a new version leaves behind.
func setCurrent(t *testing.T, s *Store, ref, digest string) {
	t.Helper()

	layout, err := s.layout(ref)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(layout, 0o700))

	index := `{"schemaVersion":2,"manifests":[{"digest":"` + digest +
		`","annotations":{"org.opencontainers.image.ref.name":"` + storeKey(ref) + `"}}]}`
	require.NoError(t, os.WriteFile(filepath.Join(layout, "index.json"), []byte(index), 0o600))
}

// writeHistory and readHistory work on the history of xorgRef.
func writeHistory(t *testing.T, s *Store, digests ...string) {
	t.Helper()

	path := s.historyPath(storeKey(xorgRef))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(digests, "\n")+"\n"), 0o600))
}

func readHistory(t *testing.T, s *Store) []string {
	t.Helper()

	h, err := s.history(storeKey(xorgRef))
	require.NoError(t, err)

	return h
}

func bundleExists(t *testing.T, s *Store, digest string) bool {
	t.Helper()

	dir, err := s.bundleDir(digest)
	require.NoError(t, err)

	_, err = os.Stat(dir)

	return err == nil
}

// gcRunner fakes umoci and records which layouts it was asked to collect.
func gcRunner(t *testing.T, s *Store) *[]string {
	t.Helper()

	var collected []string
	s.cmdRunner = func(bin string, args []string) error {
		require.Equal(t, "umoci", filepath.Base(bin), "prune must only run umoci gc: %v", args)
		require.Len(t, args, 3)
		assert.Equal(t, []string{"gc", "--layout"}, args[:2])
		collected = append(collected, filepath.Base(args[2]))
		return nil
	}

	return &collected
}

func TestRecordAppendsNewDigests(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	require.NoError(t, s.record(xorgRef))
	assert.Equal(t, []string{xorgDigest}, readHistory(t, s))

	setCurrent(t, s, xorgRef, oldDigest)
	require.NoError(t, s.record(xorgRef))
	assert.Equal(t, []string{xorgDigest, oldDigest}, readHistory(t, s))
}

// A refresh that finds nothing new must not push the previous version out
// of the history, or the next refresh would prune it while it is only N-1.
func TestRecordIgnoresAnUnchangedDigest(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	writeHistory(t, s, oldDigest, xorgDigest)

	require.NoError(t, s.record(xorgRef))
	assert.Equal(t, []string{oldDigest, xorgDigest}, readHistory(t, s))
}

// A tag moved back to a version it held before makes that version current
// again, so it moves to the end rather than appearing twice.
func TestRecordMovesARevisitedDigestToTheEnd(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	writeHistory(t, s, xorgDigest, oldDigest)

	require.NoError(t, s.record(xorgRef))
	assert.Equal(t, []string{oldDigest, xorgDigest}, readHistory(t, s))
}

func TestPruneKeepsTheLastTwoVersions(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	collected := gcRunner(t, s)

	for _, d := range []string{olderDigest, oldDigest, xorgDigest, kaliDigest} {
		newExistingBundle(t, s, d)
	}
	writeHistory(t, s, olderDigest, oldDigest, xorgDigest)

	require.NoError(t, s.Prune())

	assert.False(t, bundleExists(t, s, olderDigest))
	assert.True(t, bundleExists(t, s, oldDigest))
	assert.True(t, bundleExists(t, s, xorgDigest))
	assert.True(t, bundleExists(t, s, kaliDigest))
	assert.Equal(t, []string{oldDigest, xorgDigest}, readHistory(t, s))
	assert.ElementsMatch(t, []string{storeKey(xorgRef), storeKey(kaliRef)}, *collected)
}

// Two references can name the same image. A version that one has moved
// past is still current for the other, and must survive.
func TestPruneKeepsADigestAnotherReferenceStillUses(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	gcRunner(t, s)

	setCurrent(t, s, kaliRef, olderDigest)
	for _, d := range []string{olderDigest, oldDigest, xorgDigest} {
		newExistingBundle(t, s, d)
	}
	writeHistory(t, s, olderDigest, oldDigest, xorgDigest)

	require.NoError(t, s.Prune())

	assert.True(t, bundleExists(t, s, olderDigest))
}

// A store that predates the history holds bundles nothing accounts for.
// The current version is read from the index, and the rest go.
func TestPruneWithoutHistoryKeepsTheCurrentVersion(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	gcRunner(t, s)

	for _, d := range []string{oldDigest, xorgDigest, kaliDigest} {
		newExistingBundle(t, s, d)
	}

	require.NoError(t, s.Prune())

	assert.False(t, bundleExists(t, s, oldDigest))
	assert.True(t, bundleExists(t, s, xorgDigest))
	assert.True(t, bundleExists(t, s, kaliDigest))
}

// A pull that failed before writing the index leaves a layout with no
// current version. Its history still protects what it recorded.
func TestPruneLayoutWithoutAnIndex(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	gcRunner(t, s)

	layout, err := s.layout(xorgRef)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(layout, "index.json")))

	newExistingBundle(t, s, oldDigest)
	newExistingBundle(t, s, olderDigest)
	writeHistory(t, s, olderDigest, oldDigest)

	require.NoError(t, s.Prune())

	assert.True(t, bundleExists(t, s, oldDigest))
	assert.True(t, bundleExists(t, s, olderDigest))
}

// Deleting is not undone, so an index that cannot be read stops the prune
// rather than leaving its versions unprotected.
func TestPruneRefusesACorruptIndex(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	gcRunner(t, s)

	layout, err := s.layout(xorgRef)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(layout, "index.json"), []byte("{"), 0o600))
	newExistingBundle(t, s, xorgDigest)

	require.Error(t, s.Prune())
	assert.True(t, bundleExists(t, s, xorgDigest))
}

// A temporary unpack directory is left behind only when an unpack was
// killed. A recent one may belong to an unpack still running.
func TestPruneRemovesOnlyStaleTempDirs(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	gcRunner(t, s)

	unpacked := filepath.Join(s.Root, "unpacked")
	stale := filepath.Join(unpacked, "tmp-stale")
	fresh := filepath.Join(unpacked, "tmp-fresh")
	require.NoError(t, os.MkdirAll(stale, 0o700))
	require.NoError(t, os.MkdirAll(fresh, 0o700))

	past := time.Now().Add(-2 * staleTempAge)
	require.NoError(t, os.Chtimes(stale, past, past))

	require.NoError(t, s.Prune())

	assert.NoDirExists(t, stale)
	assert.DirExists(t, fresh)
}

// An image can ship directories without the owner write bit, and a
// rootless unpack keeps them that way, which os.RemoveAll cannot descend.
func TestPruneRemovesReadOnlyDirectories(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	gcRunner(t, s)

	dir := newExistingBundle(t, s, oldDigest)
	ro := filepath.Join(dir, "rootfs", "usr")
	require.NoError(t, os.MkdirAll(ro, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ro, "file"), nil, 0o400))
	require.NoError(t, os.Chmod(ro, 0o500))

	require.NoError(t, s.Prune())

	assert.False(t, bundleExists(t, s, oldDigest))
}

func TestPruneEmptyStore(t *testing.T) {
	t.Parallel()

	s := &Store{Root: t.TempDir()}
	s.cmdRunner = func(bin string, args []string) error {
		t.Fatalf("unexpected exec of %s %v on an empty store", bin, args)
		return nil
	}

	require.NoError(t, s.Prune())
}

// A failed gc leaves blobs behind and nothing broken, so it is reported
// without stopping the bundles from being pruned.
func TestPruneCarriesOnPastAFailedGC(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	s.cmdRunner = func(string, []string) error { return errUnpackFake }

	newExistingBundle(t, s, oldDigest)

	require.NoError(t, s.Prune())
	assert.False(t, bundleExists(t, s, oldDigest))
}

// Refreshing is what creates new versions, so it is also what prunes the
// ones that fell out of the window.
func TestPullAllPrunes(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cfg := newWarmConfig(t, s)
	s.cmdRunner = func(string, []string) error { return nil }

	newExistingBundle(t, s, olderDigest)
	newExistingBundle(t, s, oldDigest)
	writeHistory(t, s, olderDigest, oldDigest)

	require.NoError(t, pullAll(s, cfg))

	assert.False(t, bundleExists(t, s, olderDigest))
	assert.True(t, bundleExists(t, s, oldDigest))
	assert.Equal(t, []string{oldDigest, xorgDigest}, readHistory(t, s))
}

// TestPruneAgainstUmoci collects a real layout and removes a real rootless
// unpack, which the fakes above cannot vouch for.
//
// Run with: QUBESOME_TEST_OCI=1 go test ./internal/images/... -run TestPruneAgainstUmoci
func TestPruneAgainstUmoci(t *testing.T) {
	if os.Getenv("QUBESOME_TEST_OCI") == "" {
		t.Skip("set QUBESOME_TEST_OCI=1 to run against skopeo and umoci")
	}

	s := &Store{Root: t.TempDir()}

	const ref = "docker.io/library/alpine:3.20"
	_, err := refreshImage(s, ref)
	require.NoError(t, err)

	layout, err := s.layout(ref)
	require.NoError(t, err)
	require.NoError(t, s.run(files.UmociBinary, gcArgs(layout)))

	digest, err := s.Digest(ref)
	require.NoError(t, err)
	require.True(t, bundleExists(t, s, digest))

	setCurrent(t, s, ref, oldDigest)
	require.NoError(t, s.writeHistory(storeKey(ref), []string{oldDigest}))

	require.NoError(t, s.Prune())
	assert.False(t, bundleExists(t, s, digest))
}
