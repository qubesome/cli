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

func TestShowLogsFilters(t *testing.T) {
	t.Parallel()

	lines := []string{
		`msg="starting gateway"`,
		`msg="proxy decision" workload=chrome-work host=a.example action=splice`,
		`msg="proxy decision" workload=cli-llm-work host=b.example action=deny`,
		`msg="proxy decision" workload=cli-llm-personal host=c.example action=splice`,
		`msg="proxy decision" workload=chrome-personal host=d.example action=splice`,
	}

	tests := []struct {
		name     string
		profile  string
		workload string
		want     []string
	}{
		{
			name: "no filter shows the whole log",
			want: lines,
		},
		{
			name:    "a profile shows every workload in it",
			profile: "work",
			want:    []string{lines[1], lines[2]},
		},
		{
			name:     "a workload shows it in every profile",
			workload: "cli-llm",
			want:     []string{lines[2], lines[3]},
		},
		{
			name:     "both name one workload exactly",
			profile:  "work",
			workload: "cli-llm",
			want:     []string{lines[2]},
		},
		{
			name:     "a pair that ran nothing shows nothing",
			profile:  "work",
			workload: "obsidian",
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, ShowLogs(t.Context(), &out, LogOptions{
				Path:     writeLog(t, lines...),
				Profile:  tc.profile,
				Workload: tc.workload,
			}))

			var want string
			for _, l := range tc.want {
				want += l + "\n"
			}
			assert.Equal(t, want, out.String())
		})
	}
}

// The last lines of what was asked for, not the last lines of the file
// with the filter applied afterwards. Otherwise asking for one workload's
// last twenty lines shows however many of its lines happen to fall in the
// file's last twenty.
func TestShowLogsCountsTheLinesItShows(t *testing.T) {
	t.Parallel()

	lines := []string{
		`workload=cli-llm-work host=first action=splice`,
		`workload=chrome-work host=noise action=splice`,
		`workload=chrome-work host=noise action=splice`,
		`workload=chrome-work host=noise action=splice`,
		`workload=cli-llm-work host=last action=splice`,
	}

	var out bytes.Buffer
	require.NoError(t, ShowLogs(t.Context(), &out, LogOptions{
		Path:     writeLog(t, lines...),
		Workload: "cli-llm",
		Last:     2,
	}))

	assert.Equal(t, lines[0]+"\n"+lines[4]+"\n", out.String())
}

func TestShowLogsFollowsWithAFilter(t *testing.T) {
	t.Parallel()

	path := writeLog(t, `workload=cli-llm-work host=first action=splice`)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- ShowLogs(ctx, out, LogOptions{Path: path, Workload: "cli-llm", Follow: true})
	}()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Contains(c, out.String(), "host=first")
	}, time.Second, 10*time.Millisecond)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString("workload=chrome-work host=ignored action=splice\n" +
		"workload=cli-llm-work host=second action=splice\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Contains(c, out.String(), "host=second")
	}, time.Second, 10*time.Millisecond)

	assert.NotContains(t, out.String(), "host=ignored",
		"a followed line for another workload must not be printed")

	cancel()
	require.NoError(t, <-done)
}
