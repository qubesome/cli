package images

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	xorgRef    = "ghcr.io/qubesome/xorg:latest"
	kaliRef    = "ghcr.io/qubesome/kali:latest"
	xorgKey    = "ghcr-io-qubesome-xorg-latest-61412e53772e78b1"
	kaliKey    = "ghcr-io-qubesome-kali-latest-743894dca414afaf"
	xorgDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	kaliDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestStoreDigest(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "testdata"}

	d, err := s.Digest(xorgRef)
	require.NoError(t, err)
	assert.Equal(t, xorgDigest, d)
}

// Two references sharing a tag must not share an entry. Keying by tag
// alone had both resolve to whichever was pulled last, so a profile could
// silently run the other image.
func TestStoreDigestDoesNotCollideOnASharedTag(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "testdata"}

	xorg, err := s.Digest(xorgRef)
	require.NoError(t, err)

	kali, err := s.Digest(kaliRef)
	require.NoError(t, err)

	assert.Equal(t, xorgDigest, xorg)
	assert.Equal(t, kaliDigest, kali)
	assert.NotEqual(t, xorg, kali)
}

func TestStoreDigestUnknownTag(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "testdata"}

	_, err := s.Digest("ghcr.io/qubesome/xorg:missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestStoreKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"tagged", xorgRef, xorgKey},
		{"same tag, other image", kaliRef, kaliKey},
		{"registry port", "localhost:5000/xorg:v1", "localhost-5000-xorg-v1-bee0a6947471ff61"},
		{"traversal", "../../../etc/passwd", "etc-passwd-56bfa7338a2dfd1d"},
		{"nothing readable", "///", "732c4e9711639ed1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, storeKey(tt.ref))
		})
	}
}

// The key names a directory and an OCI tag, so it must be a single path
// component and must carry no colon. umoci cuts its --image at the first
// colon, so one in the key would move the split into the middle of it.
func TestStoreKeyIsASafeSinglePathComponent(t *testing.T) {
	t.Parallel()

	refs := []string{
		xorgRef,
		"../../../etc/passwd",
		"reg.example.com:5000/a/b@sha256:abc",
		"///",
		"",
		strings.Repeat("ghcr.io/very-long/", 40) + "image:tag",
		// A reference whose separators fall so that the readable half
		// ends one byte short of its bound.
		"bbbab-baa.:./a/.bbaab-b-.b.:b/:.a.aa//./a:bb-b-::b:b-bb.bbabb/..:aaaaa-.a./:.b/a-:ab/aab//:",
	}

	for _, ref := range refs {
		key := storeKey(ref)

		assert.Equal(t, key, filepath.Base(key), "key %q is not a single path component", key)
		assert.NotContains(t, key, ":")
		assert.Regexp(t, `^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`, key)
		assert.LessOrEqual(t, len(key), keyReadableMax+1+2*keyDigestBytes)
	}
}

func TestReadBundle(t *testing.T) {
	t.Parallel()

	b, err := readBundle("testdata/bundle")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join("testdata", "bundle", "rootfs"), b.Rootfs)
	assert.Equal(t, 1000, b.UID)
	assert.Equal(t, 1000, b.GID)
	assert.Equal(t, "/home/xorg-user", b.Cwd)
	assert.Equal(t, []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=/home/xorg-user"}, b.Env)
}

func TestReadBundleMissing(t *testing.T) {
	t.Parallel()

	_, err := readBundle(t.TempDir())
	require.Error(t, err)
}

func TestStoreBundleDirHostileDigest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := &Store{Root: root}

	tests := []struct {
		name   string
		digest string
	}{
		{name: "traversal", digest: "sha256:../../../../../../etc/passwd"},
		{name: "absolute", digest: "sha256:/etc/passwd"},
		{name: "empty", digest: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The digest is read out of a layout index on disk, so refusing
			// it outright says more than a path that was quietly rewritten
			// to sit inside the store.
			_, err := s.bundleDir(tc.digest)
			require.ErrorIs(t, err, files.ErrUnsafePath)
		})
	}
}

func TestStoreBundleDirIsUnderTheStoreRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s := &Store{Root: root}

	dir, err := s.bundleDir("sha256:" + strings.Repeat("a", 64))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "unpacked", "sha256-"+strings.Repeat("a", 64)), dir)
}
