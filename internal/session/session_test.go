// The holder actually starting is not covered here. Hold runs as the
// command of bwrap --unshare-user, and this development container cannot
// mount /proc in a new pid namespace, so a real sandbox does not come up.
// hack/verify-sandbox-reentry.sh answers on the host what the holder rests
// on, and its checks 7 and 8 are recorded in its header. What is left is
// testable in full: the lock, the state file the holder is found again by,
// and the argument list Enter produces.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSession(t *testing.T) Session {
	t.Helper()

	dir := t.TempDir()

	return Session{
		Dir:       dir,
		LockPath:  filepath.Join(dir, "lock"),
		StatePath: filepath.Join(dir, "holder.json"),
	}
}

func TestAcquireHoldsAgainstASecondHolder(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "lock")

	first, err := acquire(path)
	require.NoError(t, err)
	defer first.Close()

	// flock lives on the open file description, so a second open of the
	// same file is a second holder even from within this process.
	_, err = acquire(path)
	assert.ErrorIs(t, err, ErrHeld)
}

func TestAcquireAfterTheHolderLetsGo(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "lock")

	first, err := acquire(path)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	second, err := acquire(path)
	require.NoError(t, err)
	require.NoError(t, second.Close())
}

func TestAcquireMissingDir(t *testing.T) {
	t.Parallel()

	_, err := acquire(filepath.Join(t.TempDir(), "missing", "lock"))
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrHeld)
}

func TestOpenWithoutAHolder(t *testing.T) {
	t.Parallel()

	_, err := newSession(t).Open(3)
	assert.ErrorIs(t, err, ErrNoHolder)
}

func TestOpenStaleState(t *testing.T) {
	t.Parallel()

	s := newSession(t)
	require.NoError(t, sandbox.WriteState(s.StatePath, os.Getpid()))

	// A crashed holder leaves its state file behind, and the pid in it is
	// eventually given to something else. A start time that no longer
	// matches is what that looks like from here.
	staleState(t, s.StatePath)

	_, err := s.Open(3)
	assert.ErrorIs(t, err, ErrNoHolder)
}

func TestOpenRejectsAStandardStream(t *testing.T) {
	t.Parallel()

	s := newSession(t)
	require.NoError(t, sandbox.WriteState(s.StatePath, os.Getpid()))

	_, err := s.Open(2)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoHolder)
}

func TestOpenLiveState(t *testing.T) {
	t.Parallel()

	s := newSession(t)
	require.NoError(t, sandbox.WriteState(s.StatePath, os.Getpid()))

	// The test process is not a holder, but /proc/<pid>/ns/user exists for
	// it just the same, so this covers everything Open does apart from
	// which namespace it lands on.
	ns, err := s.Open(4)
	require.NoError(t, err)
	defer ns.Close()

	assert.NotNil(t, ns.File())
	assert.Equal(t, 4, ns.fd)
}

func TestEnter(t *testing.T) {
	t.Parallel()

	inner := []string{"--args", "3", "--", "/usr/bin/firefox", "--no-remote"}

	got := (&Namespace{fd: 4}).Enter(inner)

	assert.Equal(t, []string{
		"--userns", "4",
		"--dev-bind", "/", "/",
		"--", "/usr/bin/bwrap",
		"--args", "3", "--", "/usr/bin/firefox", "--no-remote",
	}, got)
}

func TestEnterLeavesItsInputAlone(t *testing.T) {
	t.Parallel()

	inner := []string{"--args", "3", "--", "/bin/true"}
	want := append([]string(nil), inner...)

	(&Namespace{fd: 3}).Enter(inner)

	assert.Equal(t, want, inner)
}

// staleState rewrites the state at path so its recorded start time no
// longer matches the pid it names.
func staleState(t *testing.T, path string) {
	t.Helper()

	s, err := sandbox.ReadState(path)
	require.NoError(t, err)
	s.StartTime++

	data, err := json.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, files.FileMode))
}
