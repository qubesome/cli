package images

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const termRef = "ghcr.io/qubesome/term:latest"

// writeWorkloads populates the workloads directory a config looks for
// under a profile name.
func writeWorkloads(t *testing.T, root, profile string, workloads map[string]string) {
	t.Helper()

	dir := filepath.Join(root, profile, "workloads")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	for name, body := range workloads {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
}

func TestConfigImages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeWorkloads(t, root, "work", map[string]string{
		"term.yaml":   "name: term\nimage: " + termRef + "\n",
		"editor.yaml": "name: editor\nimage: " + termRef + "\n",
		// A workload on a profile image adds nothing to the list.
		"shell.yaml": "name: shell\nimage: " + xorgRef + "\n",
		"notes.txt":  "not a workload file",
	})

	cfg := &types.Config{
		RootDir: root,
		Profiles: map[string]types.Profile{
			"work":     {Name: "work", Image: xorgRef},
			"personal": {Name: "personal", Image: xorgRef},
			"pentest":  {Name: "pentest", Image: kaliRef},
			"noimage":  {Name: "noimage"},
		},
	}

	imgs, err := ConfigImages(cfg)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{xorgRef, kaliRef, termRef}, imgs)
}

func TestConfigImagesNilConfig(t *testing.T) {
	t.Parallel()

	imgs, err := ConfigImages(nil)
	require.Error(t, err)
	assert.Empty(t, imgs)
}

func TestConfigImagesEmptyWorkloadFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeWorkloads(t, root, "work", map[string]string{"term.yaml": ""})

	cfg := &types.Config{
		RootDir:  root,
		Profiles: map[string]types.Profile{"work": {Name: "work", Image: xorgRef}},
	}

	_, err := ConfigImages(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty")
}

// newWarmConfig returns a config whose profile image and workload image
// are both unpacked in s, so the store can resolve every image it names.
func newWarmConfig(t *testing.T, s *Store) *types.Config {
	t.Helper()

	newExistingBundle(t, s, xorgDigest)
	newExistingBundle(t, s, kaliDigest)

	root := t.TempDir()
	writeWorkloads(t, root, "work", map[string]string{
		"term.yaml": "name: term\nimage: " + kaliRef + "\n",
	})

	return &types.Config{
		RootDir:  root,
		Profiles: map[string]types.Profile{"work": {Name: "work", Image: xorgRef}},
	}
}

// A warm store answers on its own. Profile and workload images now come
// from the same store, so nothing about a config it already holds needs
// skopeo or umoci, and none of it needs the network either.
func TestWarmStoreNeedsNoExec(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cfg := newWarmConfig(t, s)

	s.cmdRunner = func(bin string, args []string) error {
		t.Fatalf("unexpected exec of %s %v: the store holds every image in the config", bin, args)
		return nil
	}

	missing, err := missingImages(s, cfg)
	require.NoError(t, err)
	assert.Empty(t, missing)

	require.NoError(t, pullMissing(s, cfg))
}

// Refreshing is the exception: qubesome images refresh exists to refetch, so
// it reaches skopeo for every image even on the warm store above.
func TestPullAllRefreshesAWarmStore(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cfg := newWarmConfig(t, s)

	var ran []string
	s.cmdRunner = func(bin string, _ []string) error {
		ran = append(ran, filepath.Base(bin))
		return nil
	}

	require.NoError(t, pullAll(s, cfg))
	assert.Len(t, ran, 2, "expected a pull per image: %v", ran)
}

func TestMissingImagesReportsWhatTheStoreLacks(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	cfg := newWarmConfig(t, s)

	dir, err := s.bundleDir(kaliDigest)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(dir))

	missing, err := missingImages(s, cfg)
	require.NoError(t, err)
	assert.Equal(t, []string{kaliRef}, missing)
}

// A workload launch used to wait on a refresh of every image the config
// named. The refresh now belongs to starting a profile, and on-demand
// configurations do no refreshing at all, so nothing here may reach for
// skopeo or umoci.
func TestRefreshExpiredDoesNothingOnDemand(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	s.cmdRunner = func(bin string, args []string) error {
		t.Fatalf("on-demand refresh executed %s %v", bin, args)
		return nil
	}

	refreshExpired(s, &types.Config{WorkloadPullMode: types.OnDemand})
}
