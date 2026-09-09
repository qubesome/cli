package sandbox

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fakeKey = "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADAN"

// unpack reads back the options as bwrap parses them from the descriptor.
func unpack(t *testing.T, data []byte) []string {
	t.Helper()

	require.NotEmpty(t, data)
	require.Equal(t, byte(0), data[len(data)-1], "the last option is not NUL terminated")

	return strings.Split(string(data[:len(data)-1]), "\x00")
}

func pack(t *testing.T, s Spec, args []string, fd int) ([]string, []byte) {
	t.Helper()

	outer, f, err := PackArgs(s, args, fd)
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })

	data, err := io.ReadAll(f)
	require.NoError(t, err)

	return outer, data
}

func secretSpec() Spec {
	return Spec{
		Rootfs: "/store/unpacked/sha256-abc/rootfs",
		Env:    []string{"Q_MTLS_KEY=" + fakeKey, "DISPLAY=:0"},
		Mounts: []Mount{{Src: "/etc/localtime", Dst: "/etc/localtime", ReadOnly: true}},
		Args:   []string{"/usr/local/bin/qubesome", "profile-display", "--display", "21"},
	}
}

// The mTLS private key reaches bwrap through --setenv, and
// /proc/<pid>/cmdline is world readable, so no argument carrying a value
// may stay on the command line bwrap is executed with.
func TestPackArgsKeepsValuesOffTheCommandLine(t *testing.T) {
	t.Parallel()

	s := secretSpec()

	args, err := Args(s, -1)
	require.NoError(t, err)
	require.Contains(t, args, fakeKey, "the renderer no longer emits the value, so this test proves nothing")

	outer, data := pack(t, s, args, 4)

	for _, a := range outer {
		assert.NotContains(t, a, fakeKey)
	}
	assert.Contains(t, string(data), fakeKey, "the value must still reach bwrap through the descriptor")
}

// bwrap parses "--args" itself, and reads "--" inside the descriptor as
// the end of it, so both stay on the command line along with the command.
func TestPackArgsLeavesOnlyTheReferenceAndTheCommand(t *testing.T) {
	t.Parallel()

	s := secretSpec()

	args, err := Args(s, -1)
	require.NoError(t, err)

	outer, data := pack(t, s, args, 4)

	assert.Equal(t, append([]string{"--args", "4", "--"}, s.Args...), outer)
	assert.NotContains(t, unpack(t, data), "--", "a separator in the descriptor truncates it")
	assert.Equal(t, args[:len(args)-len(s.Args)-1], unpack(t, data))
}

// A mount path or an environment value may be "--", and splitting on the
// first one found would cut the list before the command.
func TestPackArgsSplitsOnLengthNotOnTheFirstSeparator(t *testing.T) {
	t.Parallel()

	s := Spec{
		Rootfs: "/rootfs",
		Env:    []string{"ODD=--"},
		Args:   []string{"/bin/sh", "-c", "true"},
	}

	args, err := Args(s, -1)
	require.NoError(t, err)

	outer, data := pack(t, s, args, 3)

	assert.Equal(t, []string{"--args", "3", "--", "/bin/sh", "-c", "true"}, outer)
	assert.Contains(t, unpack(t, data), "--")
}

func TestPackArgsTerminatesEveryOption(t *testing.T) {
	t.Parallel()

	s := Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}

	args, err := Args(s, -1)
	require.NoError(t, err)

	_, data := pack(t, s, args, 3)

	opts := args[:len(args)-2]
	assert.Equal(t, opts, unpack(t, data))
	assert.Equal(t, len(opts), bytes.Count(data, []byte{0}))
}

func TestPackArgsPreservesEmptyValues(t *testing.T) {
	t.Parallel()

	s := Spec{Rootfs: "/rootfs", Env: []string{"EMPTY="}, Args: []string{"/bin/sh"}}

	args, err := Args(s, -1)
	require.NoError(t, err)

	_, data := pack(t, s, args, 3)

	opts := unpack(t, data)
	require.Contains(t, opts, "EMPTY")
	assert.Equal(t, "", opts[len(opts)-1])
}

func TestPackArgsRejectsAStandardStream(t *testing.T) {
	t.Parallel()

	s := Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}

	args, err := Args(s, -1)
	require.NoError(t, err)

	_, _, err = PackArgs(s, args, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "standard streams")
}

func TestPackArgsRejectsAMismatchedList(t *testing.T) {
	t.Parallel()

	s := Spec{Rootfs: "/rootfs", Args: []string{"/bin/sh"}}

	_, _, err := PackArgs(s, []string{"--unshare-net"}, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command")
}
