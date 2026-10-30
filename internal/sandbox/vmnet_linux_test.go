package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One nameserver and nothing else, which is what the bwrap side writes
// into a sandbox. An image is free to ship a symlink at that path, to a
// systemd runtime directory a machine has nothing running in, so whatever
// is there is replaced rather than followed.
func TestWriteGuestResolvConfReplacesWhateverIsThere(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "resolv.conf")
	require.NoError(t, os.Symlink("/run/systemd/resolve/stub-resolv.conf", path))

	require.NoError(t, writeGuestResolvConf(path, "10.111.0.1"))

	fi, err := os.Lstat(path)
	require.NoError(t, err)
	assert.Zero(t, fi.Mode()&os.ModeSymlink, "the symlink must be gone, not followed")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "nameserver 10.111.0.1\n", string(data))
}

// A resolver library reads it as whatever uid the workload is running as
// by then, which is not necessarily the one the machine booted with.
func TestWriteGuestResolvConfIsReadableByAnyone(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "resolv.conf")
	require.NoError(t, writeGuestResolvConf(path, "10.111.0.1"))

	fi, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), fi.Mode().Perm())
}

// A machine with no gateway configures nothing, and says so by doing
// nothing rather than by failing.
func TestConfigureGuestNetworkWithNoNetworkDoesNothing(t *testing.T) {
	t.Parallel()

	require.NoError(t, configureGuestNetwork(nil))
}

// An address the guest cannot use is a machine that was composed wrong,
// and it fails the boot rather than coming up on no network with nobody
// told. The interface is never touched, so this reaches the parse and
// stops there whether or not there is an eth0 to configure.
func TestConfigureGuestNetworkRefusesAnAddressItCannotParse(t *testing.T) {
	t.Parallel()

	err := configureGuestNetwork(&vmNetwork{Address: "not-an-address", Gateway: "10.111.0.1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-an-address")
}

func TestConfigureGuestNetworkRefusesAGatewayItCannotParse(t *testing.T) {
	t.Parallel()

	err := configureGuestNetwork(&vmNetwork{Address: "10.111.0.2", Gateway: "not-an-address"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-an-address")
}
