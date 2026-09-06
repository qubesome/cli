package images

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestStoreDigest(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "testdata"}

	d, err := s.Digest("ghcr.io/qubesome/xorg:latest")
	require.NoError(t, err)
	assert.Equal(t, fixtureDigest, d)
}

func TestStoreDigestUnknownTag(t *testing.T) {
	t.Parallel()

	s := &Store{Root: "testdata"}

	_, err := s.Digest("ghcr.io/qubesome/xorg:missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref  string
		want string
	}{
		{"ghcr.io/qubesome/xorg:latest", "latest"},
		{"ghcr.io/qubesome/xorg", "latest"},
		{"localhost:5000/xorg:v1", "v1"},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tag(tt.ref))
		})
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

	dir, err := s.bundleDir("sha256:../../../../../../etc/passwd")
	require.NoError(t, err)

	rel, err := filepath.Rel(root, dir)
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(rel, ".."), "bundleDir escaped the store root: %s", dir)
}
