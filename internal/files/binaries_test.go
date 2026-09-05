package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainerRunnerBinary(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	write := func(root, name string, mode os.FileMode) string {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
		return p
	}

	podman := write(other, "podman", 0o755)
	docker := write(dir, "docker", 0o755)
	write(dir, "notexec", 0o644)

	orig := runnerDirs
	t.Cleanup(func() { runnerDirs = orig })
	runnerDirs = []string{dir, other}

	if got := ContainerRunnerBinary("docker"); got != docker {
		t.Errorf("docker: got %q, want %q", got, docker)
	}
	if got := ContainerRunnerBinary("podman"); got != podman {
		t.Errorf("podman: got %q, want %q", got, podman)
	}
	// Auto-detection prefers podman.
	if got := ContainerRunnerBinary(""); got != podman {
		t.Errorf("auto: got %q, want %q", got, podman)
	}

	runnerDirs = []string{t.TempDir()}
	if got := ContainerRunnerBinary("docker"); got != DockerBinary {
		t.Errorf("missing docker: got %q, want %q", got, DockerBinary)
	}
	if got := ContainerRunnerBinary(""); got != PodmanBinary {
		t.Errorf("missing all: got %q, want %q", got, PodmanBinary)
	}
}

func TestLookRunnerResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "docker-real")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // test binary.
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "docker")); err != nil {
		t.Fatal(err)
	}

	orig := runnerDirs
	t.Cleanup(func() { runnerDirs = orig })
	runnerDirs = []string{dir}

	got, ok := lookRunner("docker")
	if !ok {
		t.Fatal("expected the symlinked runner to be found")
	}
	if got != target {
		t.Errorf("got %q, want the resolved path %q", got, target)
	}
}

func TestLookRunnerSkipsUnexecutable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the execute bit")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	orig := runnerDirs
	t.Cleanup(func() { runnerDirs = orig })
	runnerDirs = []string{dir}

	if _, ok := lookRunner("podman"); ok {
		t.Error("expected a file this user cannot execute to be skipped")
	}
}

func TestLookRunnerSkipsBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "docker")); err != nil {
		t.Fatal(err)
	}

	orig := runnerDirs
	t.Cleanup(func() { runnerDirs = orig })
	runnerDirs = []string{dir}

	if _, ok := lookRunner("docker"); ok {
		t.Error("expected a broken symlink to be skipped")
	}
}

func TestLookRunnerSkipsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "podman"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}

	orig := runnerDirs
	t.Cleanup(func() { runnerDirs = orig })
	runnerDirs = []string{dir}

	if _, ok := lookRunner("podman"); ok {
		t.Error("expected a non-executable file to be skipped")
	}
	if _, ok := lookRunner("docker"); ok {
		t.Error("expected a directory to be skipped")
	}
}
