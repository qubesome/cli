package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/util/mtls"
	"github.com/qubesome/cli/pkg/control"
	pb "github.com/qubesome/cli/pkg/control/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	testWorkload = "shell-dev"
	testAddress  = "10.111.0.2"
	testTimeout  = 5 * time.Second
)

func TestRegisterNamesTheWorkloadBehindAnAddress(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload, "other-dev")
	c := startGateway(t, gw)

	require.NoError(t, c.Register(t.Context(), testWorkload, testAddress))

	assert.Equal(t, map[string]string{testAddress: testWorkload}, gw.mapped())
}

// Two workloads at one address means something upstream is wrong. The gateway
// refuses it, and the client has to surface that rather than carry on as if
// the address were classified.
func TestRegisterFailsWhenTheAddressIsAlreadyTaken(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload, "other-dev")
	c := startGateway(t, gw)

	require.NoError(t, c.Register(t.Context(), testWorkload, testAddress))

	err := c.Register(t.Context(), "other-dev", testAddress)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "other-dev")
	assert.Equal(t, map[string]string{testAddress: testWorkload}, gw.mapped())
}

func TestRegisterFailsForAWorkloadWithNoPolicy(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	err := c.Register(t.Context(), "not-in-the-policy", testAddress)

	require.Error(t, err)
	assert.Empty(t, gw.mapped())
}

func TestUnregisterDropsTheAddress(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	require.NoError(t, c.Register(t.Context(), testWorkload, testAddress))
	require.NoError(t, c.Unregister(t.Context(), testWorkload))

	assert.Empty(t, gw.mapped())
}

// A workload that crashed unregisters on a name the gateway may already have
// forgotten.
func TestUnregisterAcceptsAnUnknownWorkload(t *testing.T) {
	c := startGateway(t, newGateway(closedChan(), testWorkload))

	assert.NoError(t, c.Unregister(t.Context(), "never-registered"))
}

func TestReadyAnswersOnceTheGatewayIsUp(t *testing.T) {
	up := make(chan struct{})
	c := startGateway(t, newGateway(up, testWorkload))

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
	c := startGateway(t, newGateway(make(chan struct{}), testWorkload))

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	assert.Error(t, c.Ready(ctx))
}

func TestReloadAsksTheGatewayToReReadItsPolicy(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	require.NoError(t, c.Reload(t.Context()))

	assert.Equal(t, 1, gw.reloaded())
}

// A gateway older than this qubesome does not serve the call. That is a
// gateway which will never re-read its policy, which is a different answer
// from a reload the gateway refused.
func TestReloadReportsAGatewayThatCannotReload(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	gw.noReload = true
	c := startGateway(t, gw)

	err := c.Reload(t.Context())

	require.ErrorIs(t, err, ErrReloadUnsupported)
	assert.Equal(t, 0, gw.reloaded())
}

// The control channel writes the map every classification decision is made
// from, so a client the gateway's CA did not sign gets nowhere.
func TestClientWithTheWrongCAIsRefused(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	socket := listen(t, gw, newCreds(t))

	setCredsEnv(t, newCreds(t))

	err := NewClient(socket).Register(t.Context(), testWorkload, testAddress)

	require.Error(t, err)
	assert.Empty(t, gw.mapped())
}

// startGateway serves the control channel over a socket in the test's temp
// directory and returns a client that trusts it.
func startGateway(t *testing.T, gw *testGateway) *Client {
	t.Helper()

	creds := newCreds(t)
	socket := listen(t, gw, creds)
	setCredsEnv(t, creds)

	return NewClient(socket)
}

func listen(t *testing.T, gw *testGateway, creds *mtls.Credentials) string {
	t.Helper()

	return listenOn(t, gw, creds, filepath.Join(t.TempDir(), "control.sock"))
}

// listenOn serves the control channel on a given socket path, for a test that
// has to put the socket where a Gateway expects to find it.
func listenOn(t *testing.T, gw *testGateway, creds *mtls.Credentials, socket string) string {
	t.Helper()

	certPool := x509.NewCertPool()
	require.True(t, certPool.AppendCertsFromPEM(creds.CA))

	s := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{creds.ServerCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		MinVersion:   tls.VersionTLS13,
		ServerName:   control.ServerName,
	})))
	pb.RegisterGatewayControlServer(s, gw)

	lc := net.ListenConfig{}
	lis, err := lc.Listen(t.Context(), "unix", socket)
	require.NoError(t, err)

	served := make(chan error, 1)
	go func() {
		served <- s.Serve(lis)
	}()

	t.Cleanup(func() {
		s.Stop()
		require.NoError(t, <-served)
	})

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

// testGateway stands in for the gateway's side of the control channel. It
// answers the same way the gateway does, over a policy that is just the set of
// names it was built with.
type testGateway struct {
	pb.UnimplementedGatewayControlServer

	ready <-chan struct{}

	// noReload makes this a gateway built before Reload existed.
	noReload bool

	mu       sync.Mutex
	known    map[string]bool
	byAddr   map[netip.Addr]string
	addrOfWl map[string]netip.Addr
	reloads  int
}

func newGateway(ready <-chan struct{}, known ...string) *testGateway {
	gw := &testGateway{
		ready:    ready,
		known:    make(map[string]bool, len(known)),
		byAddr:   make(map[netip.Addr]string),
		addrOfWl: make(map[string]netip.Addr),
	}
	for _, name := range known {
		gw.known[name] = true
	}

	return gw
}

func (gw *testGateway) Register(_ context.Context, in *pb.RegisterRequest) (*pb.RegisterReply, error) {
	addr, err := netip.ParseAddr(in.GetAddress())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "address %q is not an IP address", in.GetAddress())
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()

	name := in.GetName()
	if !gw.known[name] {
		return nil, status.Errorf(codes.InvalidArgument, "workload %q is not in the loaded policy", name)
	}

	if existing, ok := gw.byAddr[addr]; ok {
		return nil, status.Errorf(codes.AlreadyExists, "address %s is already mapped to %s", addr, existing)
	}
	if _, ok := gw.addrOfWl[name]; ok {
		return nil, status.Errorf(codes.AlreadyExists, "workload %s is already mapped", name)
	}

	gw.byAddr[addr] = name
	gw.addrOfWl[name] = addr

	return &pb.RegisterReply{}, nil
}

func (gw *testGateway) Unregister(_ context.Context, in *pb.UnregisterRequest) (*pb.UnregisterReply, error) {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	addr, ok := gw.addrOfWl[in.GetName()]
	if ok {
		delete(gw.addrOfWl, in.GetName())
		delete(gw.byAddr, addr)
	}

	return &pb.UnregisterReply{}, nil
}

func (gw *testGateway) Reload(_ context.Context, _ *pb.ReloadRequest) (*pb.ReloadReply, error) {
	if gw.noReload {
		return nil, status.Error(codes.Unimplemented, "method Reload not implemented")
	}

	gw.mu.Lock()
	defer gw.mu.Unlock()

	gw.reloads++

	return &pb.ReloadReply{}, nil
}

func (gw *testGateway) Ready(ctx context.Context, _ *pb.ReadyRequest) (*pb.ReadyReply, error) {
	select {
	case <-gw.ready:
		return &pb.ReadyReply{}, nil
	case <-ctx.Done():
		//nolint:wrapcheck // A gRPC status is the wire representation of the failure.
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

// reloaded returns how many times the control channel asked for a reload.
func (gw *testGateway) reloaded() int {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	return gw.reloads
}

// mapped returns the address to workload map the control channel has written.
func (gw *testGateway) mapped() map[string]string {
	gw.mu.Lock()
	defer gw.mu.Unlock()

	out := make(map[string]string, len(gw.byAddr))
	for addr, name := range gw.byAddr {
		out[addr.String()] = name
	}

	return out
}

// The gateway's own refusal names the workload and the fact, and leaves
// the reader looking for a file. The name it wants is the effective one,
// the workload and the profile joined, which is not what is written at
// the top of the workload's own config, and the policy is a file the
// gateway knows nothing about. Both are the launch's to add.
func TestARefusedRegistrationNamesThePolicyFile(t *testing.T) {
	t.Parallel()

	a := &Attach{policy: "/home/user/dotfiles/qubesome/gateway.yml"}

	err := a.refused("chrome-personal", errors.New(`workload "chrome-personal" is not in the loaded policy`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "/home/user/dotfiles/qubesome/gateway.yml")
	assert.Contains(t, err.Error(), `"chrome-personal"`)
	assert.Contains(t, err.Error(), "is not in the loaded policy", "the gateway's own message must survive")
}

// With no policy path resolved there is nothing to add, and an error with
// a sentence saying so would be worse than the error on its own.
func TestARefusedRegistrationWithNoPolicyPathIsLeftAlone(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")

	assert.Equal(t, cause, (&Attach{}).refused("chrome-personal", cause))
}
