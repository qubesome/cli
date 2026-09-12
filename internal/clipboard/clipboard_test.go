package clipboard

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/execabs"
)

func TestCopy(t *testing.T) {
	tests := []struct {
		name    string
		from    *types.Profile
		to      *types.Profile
		target  string
		wantErr string
	}{
		{
			name:    "same display",
			from:    &types.Profile{Display: 1},
			to:      &types.Profile{Display: 1},
			target:  "",
			wantErr: "cannot copy clipboard within the same display",
		},
		{
			name:    "invalid type",
			from:    &types.Profile{Display: 0},
			to:      &types.Profile{Display: 1},
			target:  "foo",
			wantErr: "unsupported copy type",
		},
		{
			name:    "no target",
			from:    &types.Profile{Display: 0},
			wantErr: "target profile cannot be nil when ToHost is false",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := []command.Option[Options]{
				WithSourceProfile(tc.from),
				WithTargetProfile(tc.to),
				WithContentType(tc.target),
			}

			err := Run(opts...)

			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestCopyCommands(t *testing.T) {
	tests := []struct {
		name        string
		from        uint8
		target      uint8
		contentType string
		cookiePath  string
		wantOut     []string
		wantIn      []string
	}{
		{
			name:       "no content type",
			from:       0,
			target:     1,
			cookiePath: "/run/user/1000/qubesome/work.cookie",
			wantOut:    []string{"-selection", "clip", "-o", "-display", ":0"},
			wantIn:     []string{"-selection", "clip", "-i", "-display", ":1"},
		},
		{
			name:        "with content type",
			from:        11,
			target:      0,
			contentType: "image/png",
			cookiePath:  "/run/user/1000/qubesome/work.cookie",
			wantOut:     []string{"-selection", "clip", "-o", "-display", ":11"},
			wantIn:      []string{"-selection", "clip", "-t", "image/png", "-i", "-display", ":0"},
		},
		{
			name:       "cookie path with shell metacharacters stays one argument",
			from:       0,
			target:     1,
			cookiePath: "/tmp/a b;$(id)/'x'.cookie",
			wantOut:    []string{"-selection", "clip", "-o", "-display", ":0"},
			wantIn:     []string{"-selection", "clip", "-i", "-display", ":1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, in := copyCommands(tc.from, tc.target, tc.contentType, "", tc.cookiePath)

			assert.Equal(t, append([]string{files.XclipBinary}, tc.wantOut...), out.Args)
			assert.Equal(t, append([]string{files.XclipBinary}, tc.wantIn...), in.Args)
			assert.Contains(t, in.Env, "XAUTHORITY="+tc.cookiePath)
			assert.Nil(t, out.Env)
		})
	}
}

func TestPipeClosesBothEndsWhenStartFails(t *testing.T) {
	out := execabs.Command(files.XclipBinary, "-o")                     //nolint:gosec // fixed test command.
	in := execabs.Command(filepath.Join(t.TempDir(), "does-not-exist")) //nolint:gosec // fixed test command.

	err := pipe(out, in)
	assert.ErrorContains(t, err, "cannot start")
	assert.Nil(t, out.Process, "the writing command must not be started")

	// pipe wires both ends onto the commands, so they can be recovered
	// from there. A closed pipe reports ErrClosedPipe on both.
	w, ok := out.Stdout.(*io.PipeWriter)
	require.True(t, ok)
	r, ok := in.Stdin.(*io.PipeReader)
	require.True(t, ok)

	// Reading or writing an open pipe blocks, so run them with a deadline
	// rather than hanging the suite if the ends are ever left open again.
	assert.ErrorIs(t, closedPipeErr(t, func() error {
		_, err := w.Write([]byte("x"))
		return err
	}), io.ErrClosedPipe)

	assert.ErrorIs(t, closedPipeErr(t, func() error {
		_, err := r.Read(make([]byte, 1))
		return err
	}), io.ErrClosedPipe)
}

func closedPipeErr(t *testing.T, fn func() error) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- fn() }()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("pipe end is still open")
		return nil
	}
}

func TestPipeReportsBothFailures(t *testing.T) {
	// Both commands fail on their own. Returning only one of them would
	// hide the other, and when the reading command is the one that failed
	// the writer's error is just the broken pipe it caused.
	out := execabs.Command(files.ShBinary, "-c", "sleep 0.2; exit 4") //nolint:gosec // fixed test command.
	in := execabs.Command(files.ShBinary, "-c", "exit 3")             //nolint:gosec // fixed test command.

	err := pipe(out, in)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "exit status 3", "the reading command's failure must be reported")
	assert.Contains(t, err.Error(), "exit status 4", "the writing command's failure must be reported")
}

func TestCopyCommandsXauthorityTakesEffect(t *testing.T) {
	t.Setenv("XAUTHORITY", "/from/the/environment")

	const cookiePath = "/run/user/1000/qubesome/work.cookie"
	_, in := copyCommands(0, 1, "", "", cookiePath)

	// The value the child actually reads is what matters, not how many
	// times the key appears in the slice: os/exec keeps the last one.
	echo := execabs.Command(files.ShBinary, "-c", "printf %s \"$XAUTHORITY\"") //nolint:gosec // fixed test command.
	echo.Env = in.Env

	out, err := echo.Output()
	require.NoError(t, err)
	assert.Equal(t, cookiePath, string(out))
}

// A copy between two profiles authenticates twice, against two different
// displays. Only the writing end used to be given a cookie, so the read
// authenticated with whatever the host happened to have, which is never a
// profile's cookie, and every profile to profile copy failed with
// "failed to copy clipboard: exit status 1".
func TestCopyCommandsAuthenticatesBothEnds(t *testing.T) {
	t.Setenv("XAUTHORITY", "/from/the/environment")

	const (
		fromCookie   = "/run/user/1000/qubesome/personal/.Xserver-cookie"
		targetCookie = "/run/user/1000/qubesome/work/.Xserver-cookie"
	)

	out, in := copyCommands(1, 2, "", fromCookie, targetCookie)

	assert.Equal(t, fromCookie, xauthorityOf(t, out.Env))
	assert.Equal(t, targetCookie, xauthorityOf(t, in.Env))
}

// The host end has no cookie of qubesome's, so it keeps the environment
// it was invoked with rather than being pointed at a file that is not
// there.
func TestCopyCommandsLeavesTheHostEndAlone(t *testing.T) {
	t.Setenv("XAUTHORITY", "/from/the/environment")

	out, in := copyCommands(1, 0, "", "/run/user/1000/qubesome/personal/.Xserver-cookie", "")

	assert.Equal(t, "/run/user/1000/qubesome/personal/.Xserver-cookie", xauthorityOf(t, out.Env))
	assert.Nil(t, in.Env, "an inherited environment is what the host end needs")
}

// xauthorityOf reports the XAUTHORITY a child would actually read, which
// is the last of any duplicates rather than the first.
func xauthorityOf(t *testing.T, env []string) string {
	t.Helper()

	echo := execabs.Command(files.ShBinary, "-c", "printf %s \"$XAUTHORITY\"") //nolint:gosec // fixed test command.
	echo.Env = env

	out, err := echo.Output()
	require.NoError(t, err)

	return string(out)
}

// xclip -i forks a process to own the selection and keeps it running for
// as long as it owns one. That child inherits whatever the parent was
// given for its standard error, so a pipe there is a pipe nothing ever
// closes, and os/exec's Wait waits for the copy to reach the end of it.
// It is the whole of why a from-host copy froze.
func TestPipeReturnsWhenTheWritingCommandForks(t *testing.T) {
	t.Parallel()

	out := execabs.Command(files.ShBinary, "-c", "echo hello")                  //nolint:gosec // fixed test command.
	in := execabs.Command(files.ShBinary, "-c", "cat >/dev/null; sleep 60 & :") //nolint:gosec // fixed test command.

	done := make(chan error, 1)
	go func() { done <- pipe(out, in) }()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("pipe is still waiting for a process that has already exited")
	}
}
