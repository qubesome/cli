package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAndReadState(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox.json")

	require.NoError(t, WriteState(path, os.Getpid()))
	assert.True(t, Alive(path), "the current process must read as alive")
}

func TestAliveMissingFile(t *testing.T) {
	t.Parallel()

	assert.False(t, Alive(filepath.Join(t.TempDir(), "sandbox.json")))
}

func TestAliveRejectsMismatchedStartTime(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, WriteState(path, os.Getpid()))

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// A recycled pid belongs to a process that started later, which is
	// exactly what the start time catches.
	var s State
	require.NoError(t, unmarshalState(data, &s))
	s.StartTime++
	require.NoError(t, writeState(path, s))

	assert.False(t, Alive(path), "a pid whose start time differs is a different process")
}

func TestAliveDeadPID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox.json")

	// PID 0 never names a process that can be inspected through /proc.
	require.NoError(t, writeState(path, State{PID: 0, StartTime: 1}))
	assert.False(t, Alive(path))
}

func TestStartTimeOfSelf(t *testing.T) {
	t.Parallel()

	got, err := startTime(os.Getpid())
	require.NoError(t, err)
	assert.NotZero(t, got)
}

func TestParseStartTime(t *testing.T) {
	t.Parallel()

	// A command name containing spaces and a closing parenthesis is the
	// case that defeats naive splitting.
	line := "42 (weird ) name) S 1 42 42 0 -1 4194304 100 0 0 0 5 6 0 0 20 0 1 0 987654 0 0"
	got, err := parseStartTime(line)
	require.NoError(t, err)
	assert.Equal(t, uint64(987654), got)
}

func TestStateCarriesTheGatewayAddress(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox.json")
	written, err := WriteStateWired(path, os.Getpid(), Wiring{Address: "10.111.0.2", Session: 4026531837, Gateway: 12345})
	require.NoError(t, err)

	s, err := ReadState(path)
	require.NoError(t, err)
	assert.Equal(t, written, s)
	assert.Equal(t, os.Getpid(), s.PID)
	assert.Equal(t, "10.111.0.2", s.Address)
	assert.Equal(t, uint64(4026531837), s.Session)
	assert.Equal(t, uint64(12345), s.Gateway)
}

// A sandbox with no gateway records no address, and the field is left out
// rather than written empty, so a record from before there was one and a
// record of a workload that has none are the same bytes.
func TestStateWithoutAnAddressOmitsIt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox.json")
	require.NoError(t, WriteState(path, os.Getpid()))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "address")
}

func TestParentPIDOfSelf(t *testing.T) {
	t.Parallel()

	got, err := ParentPID(os.Getpid())
	require.NoError(t, err)
	assert.Equal(t, os.Getppid(), got)
}

func TestParseParentPID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		line    string
		want    int
		wantErr bool
	}{
		{name: "plain", line: "42 (qubesome) S 7 42 42 0", want: 7},
		{name: "command name with a paren and spaces", line: "42 (a) b (c) S 9 42", want: 9},
		{name: "no command name", line: "42 qubesome S 7", wantErr: true},
		{name: "truncated", line: "42 (qubesome) S", wantErr: true},
		{name: "not a number", line: "42 (qubesome) S x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseParentPID(tc.line)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// A reaper removes only the record of the sandbox it waited on. One that
// names another process was written by a launch that came after it, and a
// pid alone does not say so once it has been given out again.
func TestRemoveStateOf(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox-app-work.json")
	own, err := WriteStateWired(path, os.Getpid(), Wiring{})
	require.NoError(t, err)

	tests := []struct {
		name string
		own  State
	}{
		{name: "another pid", own: State{PID: own.PID + 1, StartTime: own.StartTime}},
		{name: "the same pid given out again", own: State{PID: own.PID, StartTime: own.StartTime + 1}},
	}
	for _, tc := range tests {
		removed, err := RemoveStateOf(path, tc.own)
		require.NoError(t, err, tc.name)
		assert.False(t, removed, tc.name)
		assert.FileExists(t, path, tc.name)
	}

	removed, err := RemoveStateOf(path, own)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NoFileExists(t, path)

	removed, err = RemoveStateOf(path, own)
	require.NoError(t, err)
	assert.False(t, removed)
}

// A launch writing its record holds the record's lock, so a removal that
// already read the old record cannot unlink the new one. The test holds
// the lock as that launch would, and writes the replacement under it.
func TestRemoveStateOfWaitsForAWriteInProgress(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sandbox-app-work.json")
	own, err := WriteStateWired(path, os.Getpid(), Wiring{})
	require.NoError(t, err)

	lock, err := lockState(path)
	require.NoError(t, err)

	type result struct {
		removed bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		removed, err := RemoveStateOf(path, own)
		done <- result{removed, err}
	}()

	select {
	case <-done:
		t.Fatal("the removal did not wait for the write in progress")
	case <-time.After(100 * time.Millisecond):
	}

	replacement := own
	replacement.StartTime++
	data, err := json.Marshal(replacement)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	require.NoError(t, lock.Close())

	got := <-done
	require.NoError(t, got.err)
	assert.False(t, got.removed)
	assert.FileExists(t, path)
}
