package gateway

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "gateway.log")
	var body []byte
	for _, l := range lines {
		body = append(body, l...)
		body = append(body, '\n')
	}
	require.NoError(t, os.WriteFile(path, body, 0o600))

	return path
}

func TestShowLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []string
		last  int
		want  string
	}{
		{
			name:  "the whole log",
			lines: []string{"one", "two", "three"},
			want:  "one\ntwo\nthree\n",
		},
		{
			name:  "the last lines",
			lines: []string{"one", "two", "three"},
			last:  2,
			want:  "two\nthree\n",
		},
		{
			name:  "more lines than the log holds",
			lines: []string{"one"},
			last:  10,
			want:  "one\n",
		},
		{
			name:  "an empty log",
			lines: nil,
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, ShowLogs(t.Context(), &out, LogOptions{
				Path: writeLog(t, tc.lines...),
				Last: tc.last,
			}))
			assert.Equal(t, tc.want, out.String())
		})
	}
}

// A session with no gateway has no log, and saying so is not a failure of
// the command. It is the answer to what was asked.
func TestShowLogsWithoutALog(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := ShowLogs(t.Context(), &out, LogOptions{
		Path: filepath.Join(t.TempDir(), "absent.log"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no gateway log")
	assert.Empty(t, out.String())
}

// syncBuffer is a bytes.Buffer the follower writes to while the test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestShowLogsFollows(t *testing.T) {
	t.Parallel()

	path := writeLog(t, "first")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- ShowLogs(ctx, out, LogOptions{Path: path, Follow: true})
	}()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, "first\n", out.String())
	}, time.Second, 10*time.Millisecond, "what the log already held must be printed")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("second\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, "first\nsecond\n", out.String())
	}, time.Second, 10*time.Millisecond, "a line appended while following must be printed")

	cancel()
	require.NoError(t, <-done, "a cancelled follow is how the command ends, not a failure")
}

// The log describes the gateway that is running, so starting one begins the
// log afresh rather than adding to what the last one said.
func TestOpenLogTruncates(t *testing.T) {
	t.Parallel()

	path := writeLog(t, "what the last gateway said")

	f, err := openLog(path)
	require.NoError(t, err)
	defer f.Close()

	_, err = f.WriteString("what this one says\n")
	require.NoError(t, err)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "what this one says\n", string(body))
}

func TestOpenLogIsPrivate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gateway.log")

	f, err := openLog(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	st, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm(),
		"the log carries the names a workload asked for, so it is the user's alone")
}

// The uplink joins the log the gateway sandbox's launch already began,
// rather than starting it over and dropping what explains a gateway that
// had trouble coming up.
func TestAppendLogKeepsWhatIsThere(t *testing.T) {
	t.Parallel()

	path := writeLog(t, "the gateway is starting")

	f, err := appendLog(path)
	require.NoError(t, err)
	defer f.Close()

	_, err = f.WriteString("the uplink is up\n")
	require.NoError(t, err)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "the gateway is starting\nthe uplink is up\n", string(body))
}
