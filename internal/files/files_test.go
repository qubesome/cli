package files

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureMappedDir(t *testing.T) {
	t.Parallel()

	t.Run("creates missing dir owned by the current user", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "missing")
		require.NoError(t, EnsureMappedDir(dir+"/"))

		fi, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, fi.IsDir())
		assert.Equal(t, os.FileMode(DirMode), fi.Mode().Perm())

		st, ok := fi.Sys().(*syscall.Stat_t)
		require.True(t, ok)
		assert.Equal(t, os.Getuid(), int(st.Uid))
		assert.Equal(t, os.Getgid(), int(st.Gid))
	})

	t.Run("does not create missing dir without trailing separator", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "missing")
		require.ErrorIs(t, EnsureMappedDir(dir), ErrMissingMappedPath)

		_, err := os.Stat(dir)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("does not create missing parent dirs", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		require.ErrorIs(t, EnsureMappedDir(filepath.Join(root, "a", "b")+"/"), ErrMissingMappedPath)

		_, err := os.Stat(filepath.Join(root, "a"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("leaves existing dir untouched", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o755))
		require.NoError(t, EnsureMappedDir(dir))

		fi, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
	})

	t.Run("leaves existing file untouched", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("data"), FileMode))
		require.NoError(t, EnsureMappedDir(path))

		fi, err := os.Stat(path)
		require.NoError(t, err)
		assert.False(t, fi.IsDir())
	})

	t.Run("reports the parent not being a dir", func(t *testing.T) {
		t.Parallel()

		parent := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(parent, []byte("data"), FileMode))

		err := EnsureMappedDir(filepath.Join(parent, "child") + "/")
		require.ErrorIs(t, err, syscall.ENOTDIR)
		assert.NotErrorIs(t, err, ErrMissingMappedPath)
	})

	t.Run("reports a trailing separator on a file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(path, []byte("data"), FileMode))

		err := EnsureMappedDir(path + "/")
		require.ErrorIs(t, err, syscall.ENOTDIR)
		assert.NotErrorIs(t, err, ErrMissingMappedPath)
	})

	t.Run("reports permission errors as they are", func(t *testing.T) {
		t.Parallel()

		if os.Getuid() == 0 {
			t.Skip("root bypasses dir permissions")
		}

		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "locked"), 0o000))

		err := EnsureMappedDir(filepath.Join(root, "locked", "child") + "/")
		require.ErrorIs(t, err, os.ErrPermission)
		assert.NotErrorIs(t, err, ErrMissingMappedPath)
	})
}

func TestWorkloadShmPathIsOutsideTheSharedRuntimeDir(t *testing.T) {
	t.Parallel()

	shared, err := IsolatedRunUserPath("prof")
	require.NoError(t, err)

	shm, err := WorkloadShmPath("prof", "work")
	require.NoError(t, err)

	// The shared runtime dir is mounted into every workload of a profile.
	// A workload's shared memory inside it would be reachable by all of
	// its siblings, which is what having one per workload is meant to
	// prevent.
	require.False(t, strings.HasPrefix(shm, shared+string(filepath.Separator)),
		"workload shm %q must not be inside the shared runtime dir %q", shm, shared)
}

func TestWorkloadShmPathIsPerWorkload(t *testing.T) {
	t.Parallel()

	a, err := WorkloadShmPath("prof", "alpha")
	require.NoError(t, err)

	b, err := WorkloadShmPath("prof", "beta")
	require.NoError(t, err)

	require.NotEqual(t, a, b)
}

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{name: "plain", input: "work", ok: true},
		{name: "hyphenated", input: "work-2", ok: true},
		{name: "empty", input: "", ok: false},
		{name: "traversal", input: "..", ok: false},
		{name: "traversal with a separator", input: "../other", ok: false},
		{name: "absolute", input: "/etc", ok: false},
		{name: "nested", input: "work/sub", ok: false},
		{name: "dot", input: ".", ok: false},
		{name: "hidden", input: ".ssh", ok: false},
		{name: "too long", input: strings.Repeat("a", nameMax+1), ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateName("profile name", tc.input)
			if tc.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrUnsafePath)
		})
	}
}

func TestJoinRel(t *testing.T) {
	t.Parallel()

	const base = "/base"

	tests := []struct {
		name string
		rel  string
		want string
	}{
		{name: "descends", rel: "a/b", want: "/base/a/b"},
		{name: "empty is the base itself", rel: "", want: "/base"},
		{name: "dot is the base itself", rel: ".", want: "/base"},
		{name: "traversal", rel: "../escape"},
		{name: "traversal in the middle", rel: "a/../../escape"},
		{name: "traversal on its own", rel: ".."},
		{name: "absolute", rel: "/etc/passwd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := JoinRel(base, tc.rel)
			if tc.want == "" {
				require.ErrorIs(t, err, ErrUnsafePath)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// Every profile path under the run dir is built from a name that reaches
// qubesome from a command line, a config or an RPC.
//
// HOME is what the run dir resolves from, so this test cannot run in
// parallel.
func TestProfileRunPathsRefuseAnUnsafeName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	unsafe := []string{"", "..", "../other", "/etc", "work/sub"}

	builders := map[string]func(string) (string, error){
		"ClientCookiePath":    ClientCookiePath,
		"IsolatedRunUserPath": IsolatedRunUserPath,
		"ServerCookiePath":    ServerCookiePath,
		"SocketPath":          SocketPath,
		"WorkloadShmPath":     func(p string) (string, error) { return WorkloadShmPath(p, "term") },
		"WorkloadShmWorkload": func(w string) (string, error) { return WorkloadShmPath("work", w) },
	}

	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			for _, in := range unsafe {
				_, err := build(in)
				require.ErrorIs(t, err, ErrUnsafePath, "%s(%q)", name, in)
			}

			got, err := build("work")
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(got, RunUserQubesome()+string(filepath.Separator)),
				"%s returned %q, which is outside %q", name, got, RunUserQubesome())
		})
	}
}

func TestWorkloadsDir(t *testing.T) {
	t.Parallel()

	const root = "/root"

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "under the profile path", path: "work", want: "/root/work/workloads"},
		{name: "profile at the config root", path: "", want: "/root/workloads"},
		{name: "traversal", path: "../escape"},
		{name: "absolute", path: "/etc"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := WorkloadsDir(root, tc.path)
			if tc.want == "" {
				require.ErrorIs(t, err, ErrUnsafePath)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// HOME is what the git root resolves from, so this test cannot run in
// parallel.
func TestGitDirPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("keeps a clone under the git root", func(t *testing.T) {
		got, err := GitDirPath("git@github.com:qubesome/dotfiles")
		require.NoError(t, err)
		require.Equal(t, filepath.Join(GitRoot(), "github.com/qubesome/dotfiles"), got)
	})

	t.Run("refuses a url that leaves the git root", func(t *testing.T) {
		for _, url := range []string{"../escape", "a/../../escape", ""} {
			_, err := GitDirPath(url)
			require.ErrorIs(t, err, ErrUnsafePath, "url %q", url)
		}
	})

	t.Run("passes an absolute path through untouched", func(t *testing.T) {
		got, err := GitDirPath("/srv/dotfiles")
		require.NoError(t, err)
		require.Equal(t, "/srv/dotfiles", got)
	})
}
