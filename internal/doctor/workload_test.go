package doctor

import (
	"os"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

func validWorkloadConfig(profileNames ...string) *types.Config {
	profiles := map[string]types.Profile{}
	for _, n := range profileNames {
		profiles[n] = validProfile(n)
	}

	return &types.Config{
		RootDir:  "/root",
		Profiles: profiles,
	}
}

func TestCheckWorkloadConfig(t *testing.T) {
	t.Parallel()

	t.Run("nil config fails", func(t *testing.T) {
		t.Parallel()

		c := checkWorkloadConfig(nil, "work", "term")
		require.Equal(t, "workload config", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "no qubesome config")
	})

	t.Run("unknown profile fails and lists known ones", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("work", "personal")
		c := checkWorkloadConfig(cfg, "bogus", "term")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "work")
		require.Contains(t, c.Detail, "personal")
	})

	t.Run("unknown workload fails and lists known ones for the profile", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("work")
		cfg.RootDir = t.TempDir()
		mustMkdirAll(t, cfg.RootDir+"/work/workloads")
		mustWriteFile(t, cfg.RootDir+"/work/workloads/term.yaml", "name: term\n")
		mustWriteFile(t, cfg.RootDir+"/work/workloads/editor.yaml", "name: editor\n")

		c := checkWorkloadConfig(cfg, "work", "bogus")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "term")
		require.Contains(t, c.Detail, "editor")
	})

	t.Run("found workload is ok", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("work")
		cfg.RootDir = t.TempDir()
		mustMkdirAll(t, cfg.RootDir+"/work/workloads")
		mustWriteFile(t, cfg.RootDir+"/work/workloads/term.yaml", "name: term\n")

		c := checkWorkloadConfig(cfg, "work", "term")
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func TestCheckWorkloadImage(t *testing.T) {
	t.Parallel()

	t.Run("present is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			output: map[string]fakeOutput{
				files.DockerBinary + " image inspect example.com/some/image:latest": {out: []byte("[]")},
			},
		}
		c := checkWorkloadImage(env, files.DockerBinary, "example.com/some/image:latest")
		require.Equal(t, "workload image", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("absent warns with a pull command", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{output: map[string]fakeOutput{}}
		c := checkWorkloadImage(env, files.DockerBinary, "example.com/some/image:latest")
		require.Equal(t, Warn, c.Status)
		require.Contains(t, c.Fix, files.DockerBinary+" pull example.com/some/image:latest")
	})
}

func TestCheckWorkloadProfileRunning(t *testing.T) {
	t.Parallel()

	t.Run("empty fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{output: map[string]fakeOutput{}}
		c := checkWorkloadProfileRunning(env, files.DockerBinary, "work")
		require.Equal(t, "profile running", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Fix, "qubesome start work")
	})

	t.Run("non-empty is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			output: map[string]fakeOutput{
				files.DockerBinary + " ps --filter name=qubesome-work --format {{.Names}}": {
					out: []byte("qubesome-work\n"),
				},
			},
		}
		c := checkWorkloadProfileRunning(env, files.DockerBinary, "work")
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func TestCheckWorkloadHostAccess(t *testing.T) {
	t.Parallel()

	t.Run("nothing requested is ok", func(t *testing.T) {
		t.Parallel()

		w := types.Workload{Name: "term"}
		p := validProfile("work")
		eff := w.ApplyProfile(&p)

		c := checkWorkloadHostAccess(w, eff)
		require.Equal(t, "host access", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("granted matches requested is ok", func(t *testing.T) {
		t.Parallel()

		w := types.Workload{
			Name: "term",
			HostAccess: types.HostAccess{
				Camera: true,
			},
		}
		p := validProfile("work")
		p.HostAccess.Camera = true
		eff := w.ApplyProfile(&p)

		c := checkWorkloadHostAccess(w, eff)
		require.Equal(t, OK, c.Status)
		require.Contains(t, c.Detail, "camera")
	})

	t.Run("two distinct reductions are named in the detail", func(t *testing.T) {
		t.Parallel()

		w := types.Workload{
			Name: "term",
			HostAccess: types.HostAccess{
				Camera:     true,
				Microphone: true,
			},
		}
		// Profile grants neither, so both requests are narrowed away.
		p := validProfile("work")
		eff := w.ApplyProfile(&p)

		c := checkWorkloadHostAccess(w, eff)
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "camera")
		require.Contains(t, c.Detail, "microphone")
		require.Contains(t, c.Fix, "hostAccess")
	})

	t.Run("path narrowed away is named", func(t *testing.T) {
		t.Parallel()

		w := types.Workload{
			Name: "term",
			HostAccess: types.HostAccess{
				Paths: []string{"/home/user/docs:/docs"},
			},
		}
		p := validProfile("work")
		// Profile allows some other path, so the workload's request does
		// not match and is dropped.
		p.HostAccess.Paths = []string{"/home/user/other"}
		eff := w.ApplyProfile(&p)

		c := checkWorkloadHostAccess(w, eff)
		require.Equal(t, Warn, c.Status)
		require.Contains(t, c.Detail, "/home/user/docs")
	})
}

func TestCheckWorkloadDevices(t *testing.T) {
	t.Parallel()

	t.Run("missing device fails", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{
			Workload: types.Workload{
				HostAccess: types.HostAccess{
					Devices: []string{"/dev/video0"},
				},
			},
		}
		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkWorkloadDevices(env, eff)
		require.Equal(t, "workload devices", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "/dev/video0")
	})

	t.Run("present device is ok", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{
			Workload: types.Workload{
				HostAccess: types.HostAccess{
					Devices: []string{"/dev/video0"},
				},
			},
		}
		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/dev/video0": fileInfo("video0"),
		}}
		c := checkWorkloadDevices(env, eff)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("no devices is ok", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{}
		env := &fakeEnv{}
		c := checkWorkloadDevices(env, eff)
		require.Equal(t, OK, c.Status)
	})
}

func TestCheckWorkloadPaths(t *testing.T) {
	t.Parallel()

	t.Run("missing path warns", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{
			Workload: types.Workload{
				HostAccess: types.HostAccess{
					Paths: []string{"/home/user/docs:/docs"},
				},
			},
		}
		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkWorkloadPaths(env, eff)
		require.Equal(t, "workload paths", c.Name)
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "/home/user/docs")
	})

	t.Run("present path is ok", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{
			Workload: types.Workload{
				HostAccess: types.HostAccess{
					Paths: []string{"/home/user/docs:/docs"},
				},
			},
		}
		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/home/user/docs": dirInfo("docs"),
		}}
		c := checkWorkloadPaths(env, eff)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("no paths is ok", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{}
		env := &fakeEnv{}
		c := checkWorkloadPaths(env, eff)
		require.Equal(t, OK, c.Status)
	})
}

func TestCheckWorkloadValidation(t *testing.T) {
	t.Parallel()

	t.Run("invalid workload fails", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{
			Workload: types.Workload{Name: "bad name!"},
		}
		c := checkWorkloadValidation(eff)
		require.Equal(t, "workload validation", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Detail)
	})

	t.Run("valid workload is ok", func(t *testing.T) {
		t.Parallel()

		eff := types.EffectiveWorkload{
			Workload: types.Workload{
				Name:  "term",
				Image: "example.com/some/image:latest",
			},
		}
		c := checkWorkloadValidation(eff)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func TestWorkload(t *testing.T) {
	t.Parallel()

	t.Run("nil config returns only the config check", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{}
		checks := Workload(env, nil, "docker", "work", "term")
		require.Len(t, checks, 1)
		require.Equal(t, "workload config", checks[0].Name)
		require.Equal(t, Fail, checks[0].Status)
	})

	t.Run("unknown profile returns only the config check", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{}
		cfg := validWorkloadConfig("work")
		checks := Workload(env, cfg, "docker", "bogus", "term")
		require.Len(t, checks, 1)
		require.Equal(t, "workload config", checks[0].Name)
		require.Equal(t, Fail, checks[0].Status)
	})

	t.Run("found workload runs every check", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("work")
		cfg.RootDir = t.TempDir()
		mustMkdirAll(t, cfg.RootDir+"/work/workloads")
		mustWriteFile(t, cfg.RootDir+"/work/workloads/term.yaml",
			"name: term\nimage: example.com/some/image:latest\n")

		env := &fakeEnv{
			output: map[string]fakeOutput{
				files.DockerBinary + " image inspect example.com/some/image:latest": {out: []byte("[]")},
				files.DockerBinary + " ps --filter name=qubesome-work --format {{.Names}}": {
					out: []byte("qubesome-work\n"),
				},
			},
		}

		checks := Workload(env, cfg, "docker", "work", "term")
		require.Len(t, checks, 7)

		names := []string{
			"workload config",
			"workload image",
			"profile running",
			"host access",
			"workload devices",
			"workload paths",
			"workload validation",
		}
		for i, c := range checks {
			require.Equal(t, names[i], c.Name)
		}
	})
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o755))
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
