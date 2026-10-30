package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/session"
	"github.com/stretchr/testify/require"
)

func TestOutlived(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	g := Gateway{
		StatePath: filepath.Join(dir, "gateway.json"),
		Session: session.Session{
			Dir:       dir,
			LockPath:  filepath.Join(dir, "lock"),
			StatePath: filepath.Join(dir, "holder.json"),
		},
	}
	require.NoError(t, sandbox.WriteState(g.Session.StatePath, os.Getpid()))
	require.NoError(t, sandbox.WriteState(g.StatePath, os.Getpid()))

	held, instance, err := g.identity()
	require.NoError(t, err)

	record := func(name string, w sandbox.Wiring) string {
		path := filepath.Join(dir, "sandbox-"+name+".json")
		_, err := sandbox.WriteStateWired(path, os.Getpid(), w)
		require.NoError(t, err)
		return path
	}

	current := sandbox.Wiring{Address: "10.111.0.2", Session: held, Gateway: instance}
	require.NoError(t, g.Outlived(record("this", current), "this"))
	require.NoError(t, g.Outlived(record("ungated", sandbox.Wiring{}), "ungated"))
	require.NoError(t, g.Outlived(filepath.Join(dir, "sandbox-absent.json"), "absent"))

	ended := current
	ended.Session++
	require.ErrorContains(t, g.Outlived(record("ended", ended), "ended"),
		"ended is still running wired to a gateway that has since gone")

	replaced := current
	replaced.Gateway++
	require.ErrorContains(t, g.Outlived(record("replaced", replaced), "replaced"), "has since gone")

	unnamed := current
	unnamed.Session = 0
	require.ErrorContains(t, g.Outlived(record("unnamed", unnamed), "unnamed"), "has since gone")

	stopped := g
	stopped.StatePath = filepath.Join(dir, "absent-gateway.json")
	require.ErrorContains(t, stopped.Outlived(record("orphan", current), "orphan"), "has since gone")
}
