package sandbox

import (
	"os"
	"path/filepath"
	"testing"

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
	require.NoError(t, WriteStateAddr(path, os.Getpid(), "10.111.0.2"))

	s, err := ReadState(path)
	require.NoError(t, err)
	assert.Equal(t, os.Getpid(), s.PID)
	assert.Equal(t, "10.111.0.2", s.Address)
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
