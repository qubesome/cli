package doctor

import (
	"os"
	"testing"

	"github.com/qubesome/cli/internal/types"
	envutil "github.com/qubesome/cli/internal/util/env"
	"github.com/stretchr/testify/require"
)

func TestCheckMappedPaths(t *testing.T) {
	t.Parallel()

	t.Run("none configured is ok", func(t *testing.T) {
		t.Parallel()

		c := checkMappedPaths(&fakeEnv{}, "profile paths", nil)
		require.Equal(t, "profile paths", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("present source is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/home/user/docs": dirInfo("docs"),
		}}
		c := checkMappedPaths(env, "workload paths", []string{"/home/user/docs:/docs"})
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("missing source warns and lists it", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkMappedPaths(env, "profile paths", []string{"/home/user/missing:/missing"})
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "/home/user/missing")
	})

	t.Run("a missing dir qubesome creates at start is not reported", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/home/user": dirInfo("user"),
		}}
		c := checkMappedPaths(env, "profile paths", []string{"/home/user/docs/:/docs"})
		require.Equal(t, OK, c.Status)
	})

	t.Run("a missing dir whose parent is absent is reported", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkMappedPaths(env, "profile paths", []string{"/home/user/docs/:/docs"})
		require.Equal(t, Warn, c.Status)
		require.Contains(t, c.Detail, "/home/user/docs/")
	})

	t.Run("a missing file mapping is reported even when its parent is there", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/home/user": dirInfo("user"),
		}}
		c := checkMappedPaths(env, "profile paths", []string{"/home/user/.gitconfig:/gitconfig"})
		require.Equal(t, Warn, c.Status)
		require.Contains(t, c.Detail, "/home/user/.gitconfig")
	})
}

// TestPrimeExpansion does not run in parallel, since the expansion
// mapping it primes is a package global shared with the sandbox specs.
func TestPrimeExpansion(t *testing.T) {
	t.Run("GITDIR and drive labels expand once primed", func(t *testing.T) {
		cfg := &types.Config{RootDir: "/home/user/git/config"}
		profile := types.Profile{
			ExternalDrives: []string{"qubesome-data:/dev/mapper/luks-data:/run/media/user/data"},
		}

		primeExpansion(rootSource(cfg), profile)

		require.Equal(t, "/home/user/git/config/shared", envutil.Expand("${GITDIR}/shared"))
		require.Equal(t, "/run/media/user/data/personal", envutil.Expand("${qubesome-data}/personal"))
	})

	t.Run("paths are checked expanded, not as they are written", func(t *testing.T) {
		cfg := &types.Config{RootDir: "/home/user/git/config"}
		profile := types.Profile{
			ExternalDrives: []string{"qubesome-data:/dev/mapper/luks-data:/run/media/user/data"},
			Paths: []string{
				"${GITDIR}/shared/homedir/.config:/home/xorg-user/.config",
				"${qubesome-data}/personal/homedir:/home/xorg-user",
			},
		}

		primeExpansion(rootSource(cfg), profile)

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/home/user/git/config/shared/homedir/.config": dirInfo(".config"),
			"/run/media/user/data/personal/homedir":        dirInfo("homedir"),
		}}

		c := checkMappedPaths(env, "profile paths", profile.Paths)
		require.Equal(t, OK, c.Status)
	})

	t.Run("an unexpanded variable never reaches the report", func(t *testing.T) {
		cfg := &types.Config{RootDir: "/home/user/git/config"}
		profile := types.Profile{
			Paths: []string{"${GITDIR}/absent:/absent"},
		}

		primeExpansion(rootSource(cfg), profile)

		c := checkMappedPaths(&fakeEnv{}, "profile paths", profile.Paths)
		require.Equal(t, Warn, c.Status)
		require.NotContains(t, c.Detail, "${GITDIR}")
		require.Contains(t, c.Detail, "/home/user/git/config/absent")
	})
}

func TestParseExternalDrive(t *testing.T) {
	t.Parallel()

	t.Run("label, device and mountpoint are split out", func(t *testing.T) {
		t.Parallel()

		label, device, mount, err := parseExternalDrive("data:/dev/sda1:/media/data")
		require.NoError(t, err)
		require.Equal(t, "data", label)
		require.Equal(t, "/dev/sda1", device)
		require.Equal(t, "/media/data", mount)
	})

	t.Run("anything that is not three parts is rejected", func(t *testing.T) {
		t.Parallel()

		for _, entry := range []string{"data", "data:/media/data", "data:/dev/sda1:/media/data:rw"} {
			_, _, _, err := parseExternalDrive(entry)
			require.Error(t, err, entry)
		}
	})
}
