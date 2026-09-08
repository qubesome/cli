package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/profiles"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

func validWorkloadConfig(profileNames ...string) *types.Config {
	profiles := map[string]types.Profile{}
	for _, n := range profileNames {
		p := validProfile(n)
		p.Path = n
		profiles[n] = p
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

		c := checkWorkloadConfig(nil, rootSource(nil), "work", "term")
		require.Equal(t, "workload config", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "no qubesome config")
	})

	t.Run("unknown profile fails and lists known ones", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("work", "personal")
		c := checkWorkloadConfig(cfg, rootSource(cfg), "bogus", "term")
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

		c := checkWorkloadConfig(cfg, rootSource(cfg), "work", "bogus")
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

		c := checkWorkloadConfig(cfg, rootSource(cfg), "work", "term")
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("workloads are found under the profile path, not the profile name", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("personal")
		cfg.RootDir = t.TempDir()

		p := cfg.Profiles["personal"]
		p.Path = "qubesome/personal"
		cfg.Profiles["personal"] = p

		mustMkdirAll(t, cfg.RootDir+"/qubesome/personal/workloads")
		mustWriteFile(t, cfg.RootDir+"/qubesome/personal/workloads/chrome.yaml", "name: chrome\n")

		c := checkWorkloadConfig(cfg, rootSource(cfg), "personal", "chrome")
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("an absolute profile path resolves against the config root dir", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("work")
		cfg.RootDir = t.TempDir()

		p := cfg.Profiles["work"]
		p.Path = filepath.Join(cfg.RootDir, "work")
		cfg.Profiles["work"] = p

		mustMkdirAll(t, cfg.RootDir+"/work/workloads")
		mustWriteFile(t, cfg.RootDir+"/work/workloads/term.yaml", "name: term\n")

		c := checkWorkloadConfig(cfg, rootSource(cfg), "work", "term")
		require.Equal(t, OK, c.Status)
	})

	t.Run("a workloads dir that is not there names the dir it looked in", func(t *testing.T) {
		t.Parallel()

		cfg := validWorkloadConfig("work")
		cfg.RootDir = t.TempDir()

		c := checkWorkloadConfig(cfg, rootSource(cfg), "work", "term")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, cfg.RootDir+"/work/workloads")
	})
}

func TestCheckWorkloadRunner(t *testing.T) {
	t.Parallel()

	t.Run("no runner is ok and names bwrap", func(t *testing.T) {
		t.Parallel()

		c := checkWorkloadRunner(&fakeEnv{}, "")
		require.Equal(t, "workload runner", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, "bwrap")
	})

	t.Run("firecracker with every tool is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{paths: map[string]string{
			files.FireCrackerBinary: files.FireCrackerBinary,
			files.BwrapBinary:       files.BwrapBinary,
			files.MkfsExt4Binary:    files.MkfsExt4Binary,
		}}
		c := checkWorkloadRunner(env, "firecracker")
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, files.MkfsExt4Binary)
	})

	t.Run("firecracker without mkfs fails, since it builds the rootfs", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{paths: map[string]string{
			files.FireCrackerBinary: files.FireCrackerBinary,
			files.BwrapBinary:       files.BwrapBinary,
		}}
		c := checkWorkloadRunner(env, "firecracker")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, files.MkfsExt4Binary)
		require.NotContains(t, c.Detail, files.FireCrackerBinary)
	})

	t.Run("firecracker without its own binary fails", func(t *testing.T) {
		t.Parallel()

		c := checkWorkloadRunner(&fakeEnv{}, "firecracker")
		require.Equal(t, Fail, c.Status)
		require.Contains(t, c.Detail, files.FireCrackerBinary)
	})

	t.Run("a removed runner fails and says so", func(t *testing.T) {
		t.Parallel()

		for _, runner := range []string{"docker", "podman"} {
			c := checkWorkloadRunner(&fakeEnv{}, runner)
			require.Equal(t, Fail, c.Status)
			require.NotEmpty(t, c.Fix)
			require.Contains(t, c.Detail, runner)
			require.Contains(t, c.Detail, "removed")
		}
	})

	t.Run("an unknown runner fails", func(t *testing.T) {
		t.Parallel()

		c := checkWorkloadRunner(&fakeEnv{}, "kvm")
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "kvm")
	})
}

func TestCheckWorkloadImage(t *testing.T) {
	t.Parallel()

	t.Run("in the store is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{images: map[string]bool{"example.com/some/image:latest": true}}
		c := checkWorkloadImage(env, "example.com/some/image:latest")
		require.Equal(t, "workload image", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("absent warns rather than failing, since a launch pulls it", func(t *testing.T) {
		t.Parallel()

		c := checkWorkloadImage(&fakeEnv{}, "example.com/some/image:latest")
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "example.com/some/image:latest")
	})
}

func TestCheckWorkloadProfileRunning(t *testing.T) {
	t.Parallel()

	t.Run("profile down fails, since a workload has no display to attach to", func(t *testing.T) {
		t.Parallel()

		c := checkWorkloadProfileRunning(&fakeEnv{}, "work")
		require.Equal(t, "profile running", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Fix, "qubesome start work")
	})

	t.Run("profile up is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{alive: map[string]bool{profiles.SandboxStatePath("work"): true}}
		c := checkWorkloadProfileRunning(env, "work")
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
		p.Camera = true
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

	t.Run("network none is neither dropped nor granted", func(t *testing.T) {
		t.Parallel()

		w := types.Workload{
			Name: "term",
			HostAccess: types.HostAccess{
				Network: "none",
			},
		}
		p := validProfile("work")
		eff := w.ApplyProfile(&p)

		c := checkWorkloadHostAccess(w, eff)
		require.Equal(t, OK, c.Status)
		require.NotContains(t, c.Detail, "granted")
		require.NotContains(t, c.Detail, "network")
	})

	t.Run("network narrowed away is still reported as dropped", func(t *testing.T) {
		t.Parallel()

		w := types.Workload{
			Name: "term",
			HostAccess: types.HostAccess{
				Network: "host",
			},
		}
		p := validProfile("work")
		p.Network = "bridge"
		eff := w.ApplyProfile(&p)

		c := checkWorkloadHostAccess(w, eff)
		require.Equal(t, Warn, c.Status)
		require.Contains(t, c.Detail, "network")
	})

	t.Run("network matching the profile is granted", func(t *testing.T) {
		t.Parallel()

		w := types.Workload{
			Name: "term",
			HostAccess: types.HostAccess{
				Network: "host",
			},
		}
		p := validProfile("work")
		p.Network = "host"
		eff := w.ApplyProfile(&p)

		c := checkWorkloadHostAccess(w, eff)
		require.Equal(t, OK, c.Status)
		require.Contains(t, c.Detail, "network")
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
		checks := Workload(env, nil, "work", "term")
		require.Len(t, checks, 1)
		require.Equal(t, "workload config", checks[0].Name)
		require.Equal(t, Fail, checks[0].Status)
	})

	t.Run("unknown profile returns only the config check", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{}
		cfg := validWorkloadConfig("work")
		checks := Workload(env, cfg, "bogus", "term")
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
			images: map[string]bool{"example.com/some/image:latest": true},
			alive:  map[string]bool{profiles.SandboxStatePath("work"): true},
		}

		checks := Workload(env, cfg, "work", "term")
		require.Len(t, checks, 8)

		names := []string{
			"workload config",
			"workload runner",
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
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestLoadWorkloadStaysInTheWorkloadsDir(t *testing.T) {
	t.Parallel()

	cfg := validWorkloadConfig("work")
	cfg.RootDir = t.TempDir()
	dir := filepath.Join(cfg.RootDir, "work", "workloads")
	mustMkdirAll(t, dir)
	mustWriteFile(t, filepath.Join(cfg.RootDir, "outside.yaml"), "name: outside\n")
	require.NoError(t, os.Symlink(cfg.RootDir, filepath.Join(dir, "up")))

	tests := []struct {
		name     string
		workload string
	}{
		{name: "traversal", workload: "../outside"},
		{name: "absolute", workload: "/etc/passwd"},
		{name: "empty", workload: ""},
		{name: "through a symlink out of the dir", workload: "up/outside"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadWorkload(rootSource(cfg), cfg.Profiles["work"], tc.workload)
			require.Error(t, err)
		})
	}
}

func TestLoadWorkloadReadsTheProfilesWorkload(t *testing.T) {
	t.Parallel()

	cfg := validWorkloadConfig("work")
	cfg.RootDir = t.TempDir()
	mustMkdirAll(t, filepath.Join(cfg.RootDir, "work", "workloads"))
	mustWriteFile(t, filepath.Join(cfg.RootDir, "work", "workloads", "term.yaml"), "image: example.com/term\n")

	w, err := loadWorkload(rootSource(cfg), cfg.Profiles["work"], "term")
	require.NoError(t, err)
	require.Equal(t, "term", w.Name)
	require.Equal(t, "example.com/term", w.Image)
}
