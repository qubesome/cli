package gateway

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/util/mtls"
	"github.com/qubesome/gateway/pkg/control"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testWorkload = "shell-dev"
	testAddress  = "10.111.0.2"
	testTimeout  = 5 * time.Second
	pollInterval = 5 * time.Millisecond
)

func TestRegisterNamesTheWorkloadBehindAnAddress(t *testing.T) {
	reg := newRegistry(testWorkload, "other-dev")
	c := startGateway(t, reg, closedChan())

	require.NoError(t, c.Register(t.Context(), testWorkload, testAddress))

	assert.Equal(t, map[string]string{testAddress: testWorkload}, reg.mapped())
}

// Two workloads at one address means something upstream is wrong. The gateway
// refuses it, and the client has to surface that rather than carry on as if
// the address were classified.
func TestRegisterFailsWhenTheAddressIsAlreadyTaken(t *testing.T) {
	reg := newRegistry(testWorkload, "other-dev")
	c := startGateway(t, reg, closedChan())

	require.NoError(t, c.Register(t.Context(), testWorkload, testAddress))

	err := c.Register(t.Context(), "other-dev", testAddress)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "other-dev")
	assert.Equal(t, map[string]string{testAddress: testWorkload}, reg.mapped())
}

func TestRegisterFailsForAWorkloadWithNoPolicy(t *testing.T) {
	reg := newRegistry(testWorkload)
	c := startGateway(t, reg, closedChan())

	err := c.Register(t.Context(), "not-in-the-policy", testAddress)

	require.Error(t, err)
	assert.Empty(t, reg.mapped())
}

func TestUnregisterDropsTheAddress(t *testing.T) {
	reg := newRegistry(testWorkload)
	c := startGateway(t, reg, closedChan())

	require.NoError(t, c.Register(t.Context(), testWorkload, testAddress))
	require.NoError(t, c.Unregister(t.Context(), testWorkload))

	assert.Empty(t, reg.mapped())
}

// A workload that crashed unregisters on a name the gateway may already have
// forgotten.
func TestUnregisterAcceptsAnUnknownWorkload(t *testing.T) {
	c := startGateway(t, newRegistry(testWorkload), closedChan())

	assert.NoError(t, c.Unregister(t.Context(), "never-registered"))
}

func TestReadyAnswersOnceTheGatewayIsUp(t *testing.T) {
	up := make(chan struct{})
	c := startGateway(t, newRegistry(testWorkload), up)

	answered := make(chan error, 1)
	go func() {
		answered <- c.Ready(t.Context())
	}()

	select {
	case err := <-answered:
		t.Fatalf("Ready answered before the gateway was up: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(up)

	select {
	case err := <-answered:
		require.NoError(t, err)
	case <-time.After(testTimeout):
		t.Fatal("Ready did not answer once the gateway was up")
	}
}

// readyTimeout is a ceiling, so a caller that needs to give up sooner brings
// its own deadline and that one wins.
func TestReadyGivesUpWithTheCaller(t *testing.T) {
	c := startGateway(t, newRegistry(testWorkload), make(chan struct{}))

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	assert.Error(t, c.Ready(ctx))
}

// The control channel writes the map every classification decision is made
// from, so a client the gateway's CA did not sign gets nowhere.
func TestClientWithTheWrongCAIsRefused(t *testing.T) {
	reg := newRegistry(testWorkload)
	socket := listen(t, reg, closedChan(), newCreds(t))

	setCredsEnv(t, newCreds(t))

	err := NewClient(socket).Register(t.Context(), testWorkload, testAddress)

	require.Error(t, err)
	assert.Empty(t, reg.mapped())
}

// startGateway serves the control channel over a socket in the test's temp
// directory and returns a client that trusts it.
func startGateway(t *testing.T, reg control.Registry, up <-chan struct{}) *Client {
	t.Helper()

	creds := newCreds(t)
	socket := listen(t, reg, up, creds)
	setCredsEnv(t, creds)

	return NewClient(socket)
}

func listen(t *testing.T, reg control.Registry, up <-chan struct{}, creds *mtls.Credentials) string {
	t.Helper()

	socket := filepath.Join(t.TempDir(), "control.sock")
	s := control.NewServer(reg, up)

	served := make(chan error, 1)
	go func() {
		served <- s.Listen(creds.ServerCert, creds.CA, socket)
	}()

	t.Cleanup(func() {
		s.Stop()
		require.NoError(t, <-served)
	})

	require.Eventually(t, func() bool {
		_, err := os.Stat(socket)
		return err == nil
	}, testTimeout, pollInterval, "the control socket was never created")

	return socket
}

func newCreds(t *testing.T) *mtls.Credentials {
	t.Helper()

	creds, err := mtls.NewCredentialsFor(control.ServerName)
	require.NoError(t, err)

	return creds
}

// setCredsEnv is why these tests do not run in parallel. The client reads its
// credentials from the process environment, the same way it does inside a
// sandbox.
func setCredsEnv(t *testing.T, creds *mtls.Credentials) {
	t.Helper()

	t.Setenv("Q_MTLS_CA", string(creds.CA))
	t.Setenv("Q_MTLS_CERT", string(creds.ClientPEM))
	t.Setenv("Q_MTLS_KEY", string(creds.ClientKeyPEM))
}

func closedChan() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// registry stands in for the gateway's loaded policy. It answers the same
// three questions the real one does and nothing else.
type registry struct {
	mu       sync.Mutex
	known    map[string]bool
	byAddr   map[netip.Addr]string
	addrOfWl map[string]netip.Addr
}

func newRegistry(known ...string) *registry {
	r := &registry{
		known:    make(map[string]bool, len(known)),
		byAddr:   make(map[netip.Addr]string),
		addrOfWl: make(map[string]netip.Addr),
	}
	for _, name := range known {
		r.known[name] = true
	}

	return r
}

func (r *registry) HasWorkload(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.known[name]
}

func (r *registry) SetWorkload(addr netip.Addr, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.byAddr[addr]; ok {
		return &takenError{addr: addr, name: existing}
	}
	if _, ok := r.addrOfWl[name]; ok {
		return &takenError{addr: addr, name: name}
	}

	r.byAddr[addr] = name
	r.addrOfWl[name] = addr

	return nil
}

func (r *registry) RemoveWorkload(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	addr, ok := r.addrOfWl[name]
	if !ok {
		return
	}

	delete(r.addrOfWl, name)
	delete(r.byAddr, addr)
}

// mapped returns the address to workload map the control channel has written.
func (r *registry) mapped() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]string, len(r.byAddr))
	for addr, name := range r.byAddr {
		out[addr.String()] = name
	}

	return out
}

type takenError struct {
	addr netip.Addr
	name string
}

func (e *takenError) Error() string {
	return "address " + e.addr.String() + " is already mapped to " + e.name
}
