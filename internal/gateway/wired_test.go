package gateway

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/session"
	"github.com/stretchr/testify/assert"
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

	// Records written before the gateway was named in them. The session
	// alone still says whether the gateway running in it is theirs, and one
	// that names neither was never refused.
	sessionOnly := current
	sessionOnly.Gateway = 0
	require.NoError(t, g.Outlived(record("upgraded", sessionOnly), "upgraded"))
	require.NoError(t, g.Outlived(record("ancient", sandbox.Wiring{Address: "10.111.0.2"}), "ancient"))

	sessionOnly.Session++
	require.ErrorContains(t, g.Outlived(record("upgraded-ended", sessionOnly), "upgraded-ended"), "has since gone")

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

// A gateway restarted counts addresses from the start again, so a
// workload of the new one can hold the name and address a workload of the
// old one held. The old one's cleanup, arriving late, must leave it alone.
func TestUnregisterReachesOnlyTheGatewayItWasWiredTo(t *testing.T) {
	g := newSessionGateway(t)

	creds := newCreds(t)
	require.NoError(t, g.writeCreds(creds))
	gw := newGateway(closedChan(), testWorkload)
	listenOn(t, gw, creds, g.Socket)
	runningGateway(t, g)

	session, instance, err := g.identity()
	require.NoError(t, err)

	c, err := g.Client()
	require.NoError(t, err)
	require.NoError(t, c.Register(t.Context(), testWorkload, testAddress))

	replaced := sandbox.Wiring{Address: testAddress, Session: session, Gateway: instance + 1}
	g.Unregister(t.Context(), replaced, testWorkload)
	assert.Len(t, gw.mapped(), 1)

	current := sandbox.Wiring{Address: testAddress, Session: session, Gateway: instance}
	g.Unregister(t.Context(), current, testWorkload)
	assert.Empty(t, gw.mapped())
}

func TestRegisterRefusesAGatewayStartedSinceTheAddressWasAllocated(t *testing.T) {
	g := newSessionGateway(t)

	creds := newCreds(t)
	require.NoError(t, g.writeCreds(creds))
	gw := newGateway(closedChan(), testWorkload)
	listenOn(t, gw, creds, g.Socket)
	runningGateway(t, g)

	session, instance, err := g.identity()
	require.NoError(t, err)

	addr := netip.MustParseAddr(testAddress)

	replaced := &Attach{gateway: g, Addr: addr, Session: session, Gateway: instance + 1}
	require.ErrorContains(t, replaced.Register(testWorkload), "restarted")
	assert.Empty(t, gw.mapped())

	current := &Attach{gateway: g, Addr: addr, Session: session, Gateway: instance}
	require.NoError(t, current.Register(testWorkload))
	assert.Len(t, gw.mapped(), 1)
}
