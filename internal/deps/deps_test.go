package deps

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaxUserNamespaces(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "max_user_namespaces")

	require.NoError(t, os.WriteFile(path, []byte("253453\n"), 0o600))
	n, err := readSysctl(path)
	require.NoError(t, err)
	assert.Equal(t, 253453, n)

	require.NoError(t, os.WriteFile(path, []byte("0\n"), 0o600))
	n, err = readSysctl(path)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestMaxUserNamespacesMissing(t *testing.T) {
	t.Parallel()

	_, err := readSysctl(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}

// A uaccess ACL is what gives the profile the render node without group
// membership. Without it the profile silently drops to software rendering,
// which is the failure this work exists to remove, so it is checked.
func TestHasUaccessACL(t *testing.T) {
	t.Parallel()

	acl := "# file: dev/dri/renderD128\n# owner: root\n# group: render\nuser::rw-\nuser:levi:rw-\ngroup::rw-\n"
	assert.True(t, hasUserACL(acl, "levi"))
	assert.False(t, hasUserACL(acl, "someone-else"))

	plain := "# file: dev/dri/renderD128\n# owner: root\n# group: render\nuser::rw-\ngroup::rw-\n"
	assert.False(t, hasUserACL(plain, "levi"))
}

// Every command that opens a profile or a workload builds it with the
// same three tools, and the table drifted apart once already: run,
// xdg-open and images asked for a container runner long after nothing
// used one.
func TestSandboxCommandsRequireTheSandboxTools(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"run", "xdg-open", "start"} {
		for _, tool := range sandboxTools {
			assert.Contains(t, deps[name], tool, name)
		}
	}

	// images fills the store and opens nothing, so it needs what fetches
	// and unpacks and not what would have launched the result.
	for _, tool := range imageTools {
		assert.Contains(t, deps["images"], tool)
	}
	assert.NotContains(t, deps["images"], files.BwrapBinary)
}

// mkfs.ext4 travels with firecracker and nowhere else. It is what
// builds a machine's root filesystem, so a table listing one without
// the other would either report a host as short of a tool nothing there
// runs, or let a machine fail at launch over one the table said nothing
// about.
func TestMkfsTravelsWithFirecracker(t *testing.T) {
	t.Parallel()

	for name, list := range deps {
		assert.NotContains(t, list, files.FireCrackerBinary, name)
		assert.NotContains(t, list, files.MkfsExt4Binary, name)
	}

	for name, list := range optionalDeps {
		if slices.Contains(list, files.MkfsExt4Binary) {
			assert.Contains(t, list, files.FireCrackerBinary, name)
		}
		if slices.Contains(list, files.FireCrackerBinary) {
			assert.Contains(t, list, files.MkfsExt4Binary, name)
		}
	}

	assert.Contains(t, optionalDeps["run"], files.FireCrackerBinary)
	assert.Contains(t, optionalDeps["xdg-open"], files.FireCrackerBinary)
}

// Filling the store is a fetch and an unpack. A machine's rootfs is
// built from what it unpacked, which is new, but building one is not
// filling the store and a host that only ever runs qubesome images has
// no use for a VMM.
func TestImagesDoesNotAskForTheMachineTools(t *testing.T) {
	t.Parallel()

	assert.NotContains(t, optionalDeps["images"], files.FireCrackerBinary)
	assert.NotContains(t, optionalDeps["images"], files.MkfsExt4Binary)
}

// networkTools are the binaries that would touch the wire if the gateway
// were wired from the host. Every one of them comes out of the gateway
// image instead: the veth is created and addressed by a helper that runs
// on the gateway's own root filesystem, and the uplink and the ruleset are
// the gateway's own processes. iproute2, util-linux and passt are
// therefore requirements of an image and not of a host.
var networkTools = []string{
	"ip",
	"nsenter",
	"pasta",
	"passt",
	"nft",
	"iptables",
	"iptables-nft",
	"slirp4netns",
	"dnsmasq",
	"resolvectl",
	"socat",
}

// The gateway added egress to qubesome and added nothing to what a host
// has to have installed. This is the guard on that: a change that quietly
// puts ip on the host has to delete a line here and say why, rather than
// growing the requirement unnoticed.
func TestNoNetworkToolIsAHostDependency(t *testing.T) {
	t.Parallel()

	for _, table := range []map[string][]string{deps, optionalDeps} {
		for name, list := range table {
			for _, dep := range list {
				assert.NotContains(t, networkTools, filepath.Base(dep),
					"%s must not require %s on the host, the gateway image carries it", name, dep)
			}
		}
	}
}
