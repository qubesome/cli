package qubesome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

// The workload name arrives from the command line or from a profile's RPC
// and is only validated once the workload has been read, so the read
// itself has to be what keeps it inside the profile's workloads dir.
//
// runner registers the config root with the expandable env vars, which is
// package level state, so this test does not run in parallel.
func TestRunnerWorkloadNameStaysInTheWorkloadsDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "work", "workloads")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	// A readable, valid workload outside the dir, so a name that escapes
	// would succeed rather than merely find nothing.
	require.NoError(t, os.WriteFile(filepath.Join(root, "outside.yaml"),
		[]byte("image: example.com/outside\n"), 0o600))
	require.NoError(t, os.Symlink(root, filepath.Join(dir, "up")))

	cfg := &types.Config{
		RootDir: root,
		Profiles: map[string]types.Profile{
			"work": {Name: "work", Path: "work", WindowManager: "exec awesome"},
		},
	}

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
			err := runner(WorkloadInfo{Name: tc.workload, Profile: "work", Config: cfg}, "", false)
			require.ErrorIs(t, err, ErrWorkloadConfigNotFound)
		})
	}
}

// The runner is selected from the workload, and every value is named. A
// configuration still carrying a container runner has to say so rather
// than be quietly launched under bwrap.
//
// runner registers the config root with the expandable env vars, which is
// package level state, so this test does not run in parallel.
func TestRunnerSelection(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "work", "workloads")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.yaml"),
		[]byte("image: example.com/app\ncommand: /bin/app\n"), 0o600))

	cfg := &types.Config{
		RootDir: root,
		Profiles: map[string]types.Profile{
			"work": {Name: "work", Path: "work", WindowManager: "exec awesome"},
		},
	}

	tests := []struct {
		name    string
		runner  string
		wantErr string
	}{
		{
			name:    "docker is removed",
			runner:  "docker",
			wantErr: `workload "app" asks for the "docker" runner, which has been removed`,
		},
		{
			name:    "podman is removed",
			runner:  "podman",
			wantErr: `workload "app" asks for the "podman" runner, which has been removed`,
		},
		{
			name:    "unknown runner",
			runner:  "containerd",
			wantErr: `workload "app" asks for an unknown runner "containerd"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runner(WorkloadInfo{Name: "app", Profile: "work", Config: cfg}, tc.runner, false)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
