package gateway

import (
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGatewayKeepsIdentityAcrossConfigurations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	g := Gateway{Dir: dir, LockPath: filepath.Join(dir, "lock")}
	first := (types.Workload{Name: "a-b"}).ApplyProfile(&types.Profile{Name: "c"})
	second := (types.Workload{Name: "a"}).ApplyProfile(&types.Profile{Name: "b-c"})
	require.NoError(t, g.bindIdentity(first))
	require.NoError(t, g.bindIdentity(first), "relaunch preserves the same identity")
	require.ErrorContains(t, g.bindIdentity(second), "already bound")
}
