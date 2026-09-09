package sandbox

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTimeout is long enough that a read which is going to succeed has
// succeeded, and short enough that one which is not does not hold the suite.
const testTimeout = 5 * time.Second

func TestNetnsPathNamesTheSandboxNamespace(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/proc/4242/ns/net", NetnsPath(4242))
}

// The pid is bwrap's to report. Between the launch and a nested sandbox sit
// two bwraps and a sandbox init, so which pid a veth has to be put next to
// is not something to infer from parentage.
func TestChildPIDIsWhatBwrapReported(t *testing.T) {
	t.Parallel()

	pid, err := ChildPID(reported(t, `{"child-pid": 4242}`), testTimeout)

	require.NoError(t, err)
	assert.Equal(t, 4242, pid)
}

// bwrap writes more than one field and the object it writes is what ends the
// read, not the end of the descriptor.
func TestChildPIDReadsOneObjectAndStops(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	// The write end stays open, the way the outer bwrap keeps its copy for
	// as long as the sandbox runs. Nothing here ends the read but the
	// object itself.
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	_, err = w.WriteString(`{"child-pid": 4242, "unexpected": "field"}`)
	require.NoError(t, err)

	pid, err := ChildPID(r, testTimeout)

	require.NoError(t, err)
	assert.Equal(t, 4242, pid)
}

func TestChildPIDFailsWhenNoPidWasReported(t *testing.T) {
	t.Parallel()

	_, err := ChildPID(reported(t, `{}`), testTimeout)

	require.ErrorContains(t, err, "no pid")
}

func TestChildPIDFailsOnSomethingThatIsNotAnInfoObject(t *testing.T) {
	t.Parallel()

	_, err := ChildPID(reported(t, "bwrap: execvp gateway: No such file\n"), testTimeout)

	require.ErrorContains(t, err, "failed to read the sandbox pid")
}

// A sandbox that dies before it reports anything leaves a descriptor bwrap
// still holds open, so the read has to give up on its own rather than wait
// for an end of file that is not coming.
func TestChildPIDGivesUpWhenNothingIsReported(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		r.Close()
		w.Close()
	})

	_, err = ChildPID(r, time.Millisecond)

	require.ErrorContains(t, err, "failed to read the sandbox pid")
}

// reported returns a descriptor already carrying what bwrap would have
// written to its info fd.
func reported(t *testing.T, out string) *os.File {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })

	_, err = w.WriteString(out)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	return r
}
