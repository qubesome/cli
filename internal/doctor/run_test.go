package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

// sectionByPrefix returns the checks of the first section whose title
// starts with prefix.
func sectionByPrefix(t *testing.T, r *Report, prefix string) []Check {
	t.Helper()

	for _, s := range r.Sections {
		if len(s.Title) >= len(prefix) && s.Title[:len(prefix)] == prefix {
			return s.Checks
		}
	}

	t.Fatalf("no section titled %q in %v", prefix, r.Sections)

	return nil
}

func checkByName(t *testing.T, checks []Check, name string) Check {
	t.Helper()

	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}

	t.Fatalf("no check named %q in %v", name, checks)

	return Check{}
}

// TestRunWorkingProfile drives a profile that works, laid out the way a
// real one is: its directory is not its name, its paths are written
// against ${GITDIR} and against an external drive's label, and it grants
// USB devices that are not attached.
//
// None of that stops it from starting, so none of it may be reported as
// a failure. It does not run in parallel, since it primes the expansion
// mapping, which is a package global shared with the sandbox specs.
func TestRunWorkingProfile(t *testing.T) {
	root := t.TempDir()

	profileDir := filepath.Join(root, "qubesome", "personal")
	require.NoError(t, os.MkdirAll(filepath.Join(profileDir, "workloads"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(profileDir, "workloads", "chrome.yaml"),
		[]byte("image: example.com/chrome:latest\ncommand: chrome\n"), 0o600))

	drive := filepath.Join(root, "media", "qube-data")
	homedir := filepath.Join(drive, "personal", "homedir")
	shared := filepath.Join(root, "shared", "homedir", ".config")
	require.NoError(t, os.MkdirAll(homedir, 0o755))
	require.NoError(t, os.MkdirAll(shared, 0o755))

	cfg := &types.Config{
		RootDir: root,
		Profiles: map[string]types.Profile{
			"personal": {
				Name:           "personal",
				Path:           "qubesome/personal",
				Image:          "example.com/xorg:latest",
				WindowManager:  "exec awesome",
				ExternalDrives: []string{"qubesome-data:/dev/mapper/luks-data:" + drive},
				Paths: []string{
					"${GITDIR}/shared/homedir/.config:/home/xorg-user/.config",
					"${qubesome-data}/personal/homedir:/home/xorg-user",
				},
				HostAccess: types.HostAccess{
					USBDevices: []string{"TOKEN2", "FIDO2"},
				},
			},
		},
	}

	env := &fakeEnv{
		stats: map[string]os.FileInfo{
			homedir: dirInfo("homedir"),
			shared:  dirInfo(".config"),
		},
		mounts: map[string]string{"/dev/mapper/luks-data": drive},
		usb:    map[string][]string{},
		globs:  map[string][]string{},
	}

	report := Run(env, Options{Config: cfg, Profile: "personal", Workload: "chrome"})

	profile := sectionByPrefix(t, report, "profile personal")

	require.Equal(t, OK, checkByName(t, profile, "external drives").Status)
	require.Equal(t, OK, checkByName(t, profile, "profile paths").Status)

	devices := checkByName(t, profile, "profile devices")
	require.Equal(t, Warn, devices.Status)
	require.Contains(t, devices.Detail, "TOKEN2")
	require.Contains(t, devices.Detail, "FIDO2")

	workload := sectionByPrefix(t, report, "workload personal/chrome")
	require.Equal(t, OK, checkByName(t, workload, "workload config").Status)
}

// TestRunStartedProfile drives a profile that has been started, whose
// config is reached through the run dir symlink rather than directly.
//
// A config read that way reports the run dir as its root, so everything
// built from it lands under <qubesome>/run, where a profile's workloads
// have never been. It does not run in parallel, since it primes the
// expansion mapping, which is a package global shared with the sandbox
// specs.
func TestRunStartedProfile(t *testing.T) {
	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))

	configDir := filepath.Join(repo, "qubesome")
	profileDir := filepath.Join(configDir, "personal")
	require.NoError(t, os.MkdirAll(filepath.Join(profileDir, "workloads"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(profileDir, "workloads", "chrome.yaml"),
		[]byte("image: example.com/chrome:latest\ncommand: chrome\n"), 0o600))

	shared := filepath.Join(configDir, "shared", "homedir")
	require.NoError(t, os.MkdirAll(shared, 0o755))

	// LoadConfig takes the root from the path it opened, which for a
	// started profile is the run dir the symlink lives in.
	cfg := &types.Config{
		RootDir: files.RunUserQubesome(),
		Profiles: map[string]types.Profile{
			"personal": {
				Name:          "personal",
				Path:          "personal",
				Image:         "example.com/xorg:latest",
				WindowManager: "exec awesome",
				Paths:         []string{"${GITDIR}/qubesome/shared/homedir:/home/xorg-user"},
			},
		},
	}

	env := &fakeEnv{
		links: map[string]string{
			files.ProfileConfig("personal"): filepath.Join(configDir, "qubesome.config"),
		},
		stats: map[string]os.FileInfo{
			filepath.Join(repo, ".git"): dirInfo(".git"),
			shared:                      dirInfo("homedir"),
		},
	}

	report := Run(env, Options{Config: cfg, Profile: "personal", Workload: "chrome"})

	profile := sectionByPrefix(t, report, "profile personal")

	src := checkByName(t, profile, "profile source")
	require.Contains(t, src.Detail, configDir)
	require.Contains(t, src.Detail, repo)
	require.NotContains(t, src.Detail, files.RunUserQubesome())

	// ${GITDIR} is the repository a start sourced the config from, not
	// the directory the config sits in.
	require.Equal(t, OK, checkByName(t, profile, "profile paths").Status)

	workload := sectionByPrefix(t, report, "workload personal/chrome")
	require.Equal(t, OK, checkByName(t, workload, "workload config").Status)
}
