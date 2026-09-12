package bwrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The endpoint is not written into the file. It is read from the
// environment by the command the file names, so the two cannot drift into
// disagreeing about which gateway this workload has.
func TestSSHGatewayConfigNamesTheTunnel(t *testing.T) {
	t.Parallel()

	got := sshGatewayConfig()

	assert.Contains(t, got, "Host *")
	assert.Contains(t, got, "ProxyCommand "+files.InProfileBinary+" tunnel %h %p")
	assert.NotContains(t, got, "3128")
}

// A workload on the gateway reaches port 22 by asking for a tunnel, and
// nothing in an image says so. The drop-in is what tells its ssh, and it
// lands where the system config already includes from.
func TestSpecGivesAGatewayWorkloadAnSSHConfig(t *testing.T) {
	t.Parallel()

	in := gatewayInput()
	in.GatewayProxy = "10.111.0.1:3128"

	args := render(t, in)

	src := filepath.Join(in.ProfileDir, sshConfigFile)

	i := indexOfArg(args, "--ro-bind", src)
	require.NotEqual(t, -1, i, "the ssh drop-in is not mounted")
	assert.Equal(t, sshConfigDst, args[i+2])
}

// A workload with no gateway has nothing to tunnel through, and a
// ProxyCommand pointing at a proxy that is not there would break the ssh
// that works today.
func TestSpecWithoutAGatewayHasNoSSHConfig(t *testing.T) {
	t.Parallel()

	args := render(t, plainInput())

	assert.NotContains(t, args, sshConfigDst)
}

// The launch writes the file the sandbox then mounts, so a profile
// directory that does not exist yet is not a reason for the launch to
// fail.
func TestWriteSSHConfig(t *testing.T) {
	t.Parallel()

	in := gatewayInput()
	in.ProfileDir = filepath.Join(t.TempDir(), "work")

	require.NoError(t, writeSSHConfig(in))

	got, err := os.ReadFile(filepath.Join(in.ProfileDir, sshConfigFile))
	require.NoError(t, err)
	assert.Equal(t, sshGatewayConfig(), string(got))
}

// A file left by an older launch is mounted as it is, so what is there
// afterwards has to be what this version writes.
func TestWriteSSHConfigReplacesAnOlderOne(t *testing.T) {
	t.Parallel()

	in := gatewayInput()
	in.ProfileDir = t.TempDir()

	path := filepath.Join(in.ProfileDir, sshConfigFile)
	require.NoError(t, os.WriteFile(path, []byte("ProxyCommand socat -\n"), files.FileMode))

	require.NoError(t, writeSSHConfig(in))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, sshGatewayConfig(), string(got))
}
