package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

// rootSource is the source a config that was read directly resolves to,
// which is what most tests want.
func rootSource(cfg *types.Config) source {
	if cfg == nil {
		return source{}
	}

	return source{
		config: filepath.Join(cfg.RootDir, "qubesome.config"),
		root:   cfg.RootDir,
		gitDir: cfg.RootDir,
	}
}

func TestResolveSource(t *testing.T) {
	t.Parallel()

	t.Run("a config read directly keeps its own root", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{RootDir: "/home/user/git/dotfiles/qubesome"}
		src := resolveSource(&fakeEnv{}, cfg, "personal")

		require.Equal(t, "/home/user/git/dotfiles/qubesome", src.root)
		require.Equal(t, "/home/user/git/dotfiles/qubesome/qubesome.config", src.config)
	})

	t.Run("a started profile resolves back through its symlink", func(t *testing.T) {
		t.Parallel()

		target := "/home/user/git/dotfiles/qubesome/qubesome.config"
		env := &fakeEnv{links: map[string]string{
			files.ProfileConfig("personal"): target,
		}}

		cfg := &types.Config{RootDir: files.RunUserQubesome()}
		src := resolveSource(env, cfg, "personal")

		require.Equal(t, target, src.config)
		require.Equal(t, "/home/user/git/dotfiles/qubesome", src.root)
	})

	t.Run("a run dir root with no symlink is left as it is", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{RootDir: files.RunUserQubesome()}
		src := resolveSource(&fakeEnv{}, cfg, "personal")

		require.Equal(t, files.RunUserQubesome(), src.root)
	})

	t.Run("GITDIR is the repository, not the dir the config sits in", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/home/user/git/dotfiles/.git": dirInfo(".git"),
		}}

		cfg := &types.Config{RootDir: "/home/user/git/dotfiles/qubesome"}
		src := resolveSource(env, cfg, "personal")

		require.Equal(t, "/home/user/git/dotfiles/qubesome", src.root)
		require.Equal(t, "/home/user/git/dotfiles", src.gitDir)
	})

	t.Run("a config outside a repository expands GITDIR to its own root", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{RootDir: "/etc/qubesome"}
		src := resolveSource(&fakeEnv{}, cfg, "personal")

		require.Equal(t, "/etc/qubesome", src.gitDir)
	})
}
