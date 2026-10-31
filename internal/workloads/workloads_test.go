package workloads

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeRecord(t *testing.T, runDir, profile, workload string, pid int, addr string) string {
	t.Helper()

	dir := filepath.Join(runDir, profile)
	require.NoError(t, os.MkdirAll(dir, 0o700))

	path := filepath.Join(dir, "sandbox-"+workload+"-"+profile+".json")
	_, err := sandbox.WriteStateWired(path, pid, sandbox.Wiring{Address: addr})
	require.NoError(t, err)

	return path
}

// A record whose start time no longer matches its pid is one left behind
// by a sandbox that has gone.
func makeStale(t *testing.T, path string) {
	t.Helper()

	s, err := sandbox.ReadState(path)
	require.NoError(t, err)
	s.StartTime++

	data, err := json.Marshal(s)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func TestList(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	pid := os.Getpid()

	writeRecord(t, runDir, "work", "chrome", pid, "10.111.0.2")
	writeRecord(t, runDir, "work", "dev-tools", pid, "")
	makeStale(t, writeRecord(t, runDir, "personal", "slack", pid, ""))

	// The profile's own record and anything that is not a workload record
	// are not workloads.
	require.NoError(t, sandbox.WriteState(filepath.Join(runDir, "work", "sandbox.json"), pid))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "work", "notes.json"), []byte("{}"), 0o600))

	got, err := List(runDir)
	require.NoError(t, err)

	want := []Record{
		{Profile: "personal", Workload: "slack", Name: "slack-personal", PID: pid},
		{Profile: "work", Workload: "chrome", Name: "chrome-work", PID: pid, Address: "10.111.0.2", Running: true},
		{Profile: "work", Workload: "dev-tools", Name: "dev-tools-work", PID: pid, Running: true},
	}
	for i := range got {
		got[i].Path = ""
	}
	assert.Equal(t, want, got)
}

func TestListWithNoRunDir(t *testing.T) {
	t.Parallel()

	got, err := List(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListSkipsAnUnreadableRecord(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "work"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "work", "sandbox-x-work.json"), []byte("not json"), 0o600))
	writeRecord(t, runDir, "work", "chrome", os.Getpid(), "")

	got, err := List(runDir)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "chrome", got[0].Workload)
}

func TestFind(t *testing.T) {
	t.Parallel()

	records := []Record{
		{Profile: "personal", Workload: "chrome"},
		{Profile: "work", Workload: "chrome"},
		{Profile: "work", Workload: "slack"},
	}

	tests := []struct {
		name     string
		workload string
		profile  string
		want     []Record
	}{
		{name: "in every profile", workload: "chrome", want: records[:2]},
		{name: "in one profile", workload: "chrome", profile: "work", want: records[1:2]},
		{name: "nowhere", workload: "firefox"},
		{name: "not in that profile", workload: "slack", profile: "personal"},
		{name: "every workload of a profile", profile: "work", want: records[1:]},
		{name: "every workload", want: records},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, Find(records, tc.workload, tc.profile))
		})
	}
}

// The pid a record names is either a sandbox's init, which is pid 1 of a
// namespace of its own, or a process in ours with the sandbox somewhere
// below it. Only a SIGKILL to the init takes the whole sandbox down.
func TestTargets(t *testing.T) {
	t.Parallel()

	const self = "pid:[1]"

	tests := []struct {
		name     string
		pid      int
		ns       map[int]string
		children map[int][]int
		want     []int
	}{
		{
			name: "the sandbox init",
			pid:  10,
			ns:   map[int]string{10: "pid:[2]"},
			want: []int{10},
		},
		{
			name:     "a bwrap with the init below it",
			pid:      10,
			ns:       map[int]string{10: self, 11: "pid:[2]"},
			children: map[int][]int{10: {11}},
			want:     []int{11, 10},
		},
		{
			name:     "a nested launch recorded by its outer bwrap",
			pid:      10,
			ns:       map[int]string{10: self, 11: self, 12: "pid:[2]", 13: "pid:[2]"},
			children: map[int][]int{10: {11}, 11: {12}, 12: {13}},
			want:     []int{12, 10},
		},
		{
			name: "a process with nothing below it",
			pid:  10,
			ns:   map[int]string{10: self},
			want: []int{10},
		},
		{
			name: "a pid whose namespace cannot be read",
			pid:  10,
			want: []int{10},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			nsOf := func(pid int) (string, error) {
				ns, ok := tc.ns[pid]
				if !ok {
					return "", os.ErrNotExist
				}
				return ns, nil
			}

			assert.Equal(t, tc.want, targets(tc.pid, self, nsOf, tc.children))
		})
	}
}

func TestKill(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "sleep", "60")
	require.NoError(t, cmd.Start())
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	path := writeRecord(t, t.TempDir(), "work", "sleeper", cmd.Process.Pid, "")

	require.NoError(t, Kill(Record{Path: path, PID: cmd.Process.Pid, Running: true}))

	select {
	case err := <-exited:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the workload is still running")
	}
	assert.NoFileExists(t, path)
}

func TestKillRemovesAStaleRecord(t *testing.T) {
	t.Parallel()

	path := writeRecord(t, t.TempDir(), "work", "gone", os.Getpid(), "")
	makeStale(t, path)

	require.NoError(t, Kill(Record{Path: path, PID: os.Getpid()}))
	assert.NoFileExists(t, path)
}

// The pid that was listed is the one that would be signalled, and once the
// record names another it may be any process at all.
func TestKillRefusesARecordStartedAgain(t *testing.T) {
	t.Parallel()

	path := writeRecord(t, t.TempDir(), "work", "chrome", os.Getpid(), "")

	err := Kill(Record{Path: path, PID: os.Getpid() + 1, Running: true})
	require.ErrorContains(t, err, "was started again")
	assert.FileExists(t, path)
}

func TestKillARecordAlreadyGone(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox-chrome-work.json")
	require.NoError(t, Kill(Record{Path: path, PID: 1234}))
}

func TestOne(t *testing.T) {
	t.Parallel()

	records := []Record{
		{Profile: "personal", Workload: "chrome"},
		{Profile: "work", Workload: "chrome"},
		{Profile: "work", Workload: "slack"},
	}

	got, err := One(records, "slack", "")
	require.NoError(t, err)
	assert.Equal(t, records[2], got)

	got, err = One(records, "chrome", "work")
	require.NoError(t, err)
	assert.Equal(t, records[1], got)

	_, err = One(records, "chrome", "")
	require.ErrorContains(t, err, `workload "chrome" is recorded in profiles personal, work: pick one with --profile`)

	_, err = One(records, "firefox", "")
	require.ErrorIs(t, err, ErrNotRecorded)
}
