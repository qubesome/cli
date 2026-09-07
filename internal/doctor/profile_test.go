package doctor

import (
	"os"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

func validProfile(name string) types.Profile {
	return types.Profile{
		Name:          name,
		WindowManager: "exec awesome",
		Image:         "example.com/some/image:latest",
	}
}

func TestCheckProfileConfig(t *testing.T) {
	t.Parallel()

	t.Run("nil config fails", func(t *testing.T) {
		t.Parallel()

		c := checkProfileConfig(nil, "work")
		require.Equal(t, "profile config", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "no qubesome config")
	})

	t.Run("unknown profile fails and lists known ones", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{
			Profiles: map[string]types.Profile{
				"work":     validProfile("work"),
				"personal": validProfile("personal"),
			},
		}
		c := checkProfileConfig(cfg, "bogus")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "work")
		require.Contains(t, c.Detail, "personal")
	})

	t.Run("profile failing validation fails", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{
			Profiles: map[string]types.Profile{
				"work": {Name: "work"},
			},
		}
		c := checkProfileConfig(cfg, "work")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.NotEmpty(t, c.Detail)
	})

	t.Run("valid profile is ok", func(t *testing.T) {
		t.Parallel()

		cfg := &types.Config{
			Profiles: map[string]types.Profile{
				"work": validProfile("work"),
			},
		}
		c := checkProfileConfig(cfg, "work")
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func TestCheckProfileImage(t *testing.T) {
	t.Parallel()

	t.Run("present is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			output: map[string]fakeOutput{
				files.DockerBinary + " image inspect example.com/some/image:latest": {out: []byte("[{}]")},
			},
		}
		c := checkProfileImage(env, files.DockerBinary, "example.com/some/image:latest")
		require.Equal(t, "profile image", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("absent warns with pull command", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{output: map[string]fakeOutput{}}
		c := checkProfileImage(env, files.DockerBinary, "example.com/some/image:latest")
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Fix, "example.com/some/image:latest")
	})
}

func TestCheckProfileContainer(t *testing.T) {
	t.Parallel()

	t.Run("not running warns", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{output: map[string]fakeOutput{}}
		c, status := checkProfileContainer(env, files.DockerBinary, "work")
		require.Equal(t, "profile container", c.Name)
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Fix, "qubesome start")
		require.Equal(t, containerNotRunning, status)
	})

	t.Run("up is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			output: map[string]fakeOutput{
				files.DockerBinary + " ps -a --filter name=qubesome-work --format {{.Names}} {{.Status}}": {
					out: []byte("qubesome-work Up 5 minutes\n"),
				},
			},
		}
		c, status := checkProfileContainer(env, files.DockerBinary, "work")
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, "Up 5 minutes")
		require.Equal(t, containerUp, status)
	})

	t.Run("exited fails and carries the status", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			output: map[string]fakeOutput{
				files.DockerBinary + " ps -a --filter name=qubesome-work --format {{.Names}} {{.Status}}": {
					out: []byte("qubesome-work Exited (1) 2 minutes ago\n"),
				},
			},
		}
		c, status := checkProfileContainer(env, files.DockerBinary, "work")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "Exited (1) 2 minutes ago")
		require.Contains(t, c.Fix, "-i")
		require.Equal(t, containerExited, status)
	})
}

func TestCheckProfileSocket(t *testing.T) {
	t.Parallel()

	sockPath, err := files.SocketPath("work")
	require.NoError(t, err)

	t.Run("missing while up fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkProfileSocket(env, "work", containerUp)
		require.Equal(t, "profile socket", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("missing while not up is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkProfileSocket(env, "work", containerNotRunning)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("present but not a socket fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{sockPath: fileInfo("qube.sock")}}
		c := checkProfileSocket(env, "work", containerUp)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("present while not up fails as leftover", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{sockPath: socketInfo("qube.sock")}}
		c := checkProfileSocket(env, "work", containerNotRunning)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "gone")
	})

	t.Run("present and up is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{sockPath: socketInfo("qube.sock")}}
		c := checkProfileSocket(env, "work", containerUp)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func TestCheckProfileCookies(t *testing.T) {
	t.Parallel()

	serverPath, err := files.ServerCookiePath("work")
	require.NoError(t, err)
	clientPath, err := files.ClientCookiePath("work")
	require.NoError(t, err)

	t.Run("not up is ok regardless of state", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkProfileCookies(env, "work", containerNotRunning)
		require.Equal(t, "profile cookies", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("missing while up fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			clientPath: sizedFileInfo("client", 100),
		}}
		c := checkProfileCookies(env, "work", containerUp)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, serverPath)
	})

	t.Run("zero sized while up fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			serverPath: sizedFileInfo("server", 0),
			clientPath: sizedFileInfo("client", 100),
		}}
		c := checkProfileCookies(env, "work", containerUp)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, serverPath)
	})

	t.Run("both fine while up is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			serverPath: sizedFileInfo("server", 100),
			clientPath: sizedFileInfo("client", 100),
		}}
		c := checkProfileCookies(env, "work", containerUp)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func TestCheckExternalDrives(t *testing.T) {
	t.Parallel()

	t.Run("none configured is ok", func(t *testing.T) {
		t.Parallel()

		c := checkExternalDrives(&fakeEnv{}, nil)
		require.Equal(t, "external drives", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("mounted drive is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{mounts: map[string]string{
			"/dev/mapper/luks-data": "/run/media/user/data",
		}}
		c := checkExternalDrives(env, []string{"data:/dev/mapper/luks-data:/run/media/user/data"})
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("mountpoint present but nothing mounted on it fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats:  map[string]os.FileInfo{"/run/media/user/data": dirInfo("data")},
			mounts: map[string]string{},
		}
		c := checkExternalDrives(env, []string{"data:/dev/mapper/luks-data:/run/media/user/data"})
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "/dev/mapper/luks-data")
		require.Contains(t, c.Detail, "/run/media/user/data")
	})

	t.Run("mounted drive whose mountpoint cannot be statted is still ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats:  map[string]os.FileInfo{},
			mounts: map[string]string{"/dev/mapper/luks-data": "/run/media/user/data"},
		}
		c := checkExternalDrives(env, []string{"data:/dev/mapper/luks-data:/run/media/user/data"})
		require.Equal(t, OK, c.Status)
	})

	t.Run("drive mounted somewhere else fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{mounts: map[string]string{
			"/dev/mapper/luks-data": "/mnt/elsewhere",
		}}
		c := checkExternalDrives(env, []string{"data:/dev/mapper/luks-data:/run/media/user/data"})
		require.Equal(t, Fail, c.Status)
	})

	t.Run("entry that is not label:device:mountpoint fails and names the entry", func(t *testing.T) {
		t.Parallel()

		c := checkExternalDrives(&fakeEnv{}, []string{"data:/run/media/user/data"})
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "data:/run/media/user/data")
	})

	t.Run("every unmounted drive is named, not just the first", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{mounts: map[string]string{}}
		c := checkExternalDrives(env, []string{
			"data:/dev/mapper/luks-data:/run/media/user/data",
			"backup:/dev/mapper/luks-backup:/run/media/user/backup",
		})
		require.Equal(t, Fail, c.Status)
		require.Contains(t, c.Detail, "data")
		require.Contains(t, c.Detail, "backup")
	})
}

func TestCheckProfileDisplay(t *testing.T) {
	t.Parallel()

	t.Run("present while up is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/tmp/.X11-unix/X5": fileInfo("X5"),
		}}
		c := checkProfileDisplay(env, 5, containerUp)
		require.Equal(t, "display", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("present while not up warns about collision", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/tmp/.X11-unix/X5": fileInfo("X5"),
		}}
		c := checkProfileDisplay(env, 5, containerNotRunning)
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("missing while up fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkProfileDisplay(env, 5, containerUp)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("missing while not up is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkProfileDisplay(env, 5, containerNotRunning)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func TestProfile(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns only the config check", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{}
		checks := Profile(env, nil, "docker", "work")
		require.Len(t, checks, 1)
		require.Equal(t, "profile config", checks[0].Name)
		require.Equal(t, Fail, checks[0].Status)
	})

	t.Run("unknown profile returns only the config check", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{}
		cfg := &types.Config{Profiles: map[string]types.Profile{"work": validProfile("work")}}
		checks := Profile(env, cfg, "docker", "bogus")
		require.Len(t, checks, 1)
	})

	t.Run("valid profile runs every check", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats:  map[string]os.FileInfo{},
			output: map[string]fakeOutput{},
		}
		cfg := &types.Config{Profiles: map[string]types.Profile{"work": validProfile("work")}}
		checks := Profile(env, cfg, "docker", "work")
		require.Len(t, checks, 10)

		names := []string{
			"profile config",
			"profile source",
			"profile image",
			"profile container",
			"profile socket",
			"profile cookies",
			"profile paths",
			"profile devices",
			"external drives",
			"display",
		}
		for i, c := range checks {
			require.Equal(t, names[i], c.Name)
		}
	})
}
