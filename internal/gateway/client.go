// Package gateway speaks to the session gateway over its control socket.
//
// The gateway does not discover anything about the workloads behind it.
// qubesome starts them, so it knows the name and the address of each one, and
// it says so here.
package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/qubesome/cli/pkg/control"
	pb "github.com/qubesome/cli/pkg/control/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// ErrReloadUnsupported reports a gateway that does not serve Reload, which
// is one built before the call existed. It re-reads its policy never, which
// is what it did before there was anything to ask.
var ErrReloadUnsupported = errors.New("gateway: this gateway does not support reloading its policy")

// writeTimeout bounds Register, Unregister and Reload.
//
// Register and Unregister are one write to a map the gateway already holds in
// memory. There is no work behind either of them, so a round trip over a unix
// socket is all they cost. Anything longer means the gateway is not answering,
// and waiting on it will not change that.
//
// Reload belongs with them rather than with Ready. Re-reading a small policy
// file the gateway already has bound into its sandbox is not a launch, and
// nothing behind it pulls an image or programs a ruleset.
const writeTimeout = 5 * time.Second

// readyTimeout bounds the wait for the gateway to finish coming up.
//
// Ready blocks on the gateway's side until its DNS resolver, its proxy and
// its netfilter ruleset are all up. Behind that is a sandbox qubesome has just
// started from an image, which is pulled and unpacked first when it is not
// already on disk. That is minutes on a cold image, so a deadline sized for a
// round trip expires while the host is still working, and the caller reports a
// gateway that is starting normally as a failure.
//
// It is a ceiling rather than an estimate, for the same reason launchTimeout
// in internal/inception is. Nothing here should take ten minutes. A caller
// that waits a little too long still gets its answer, while one that gives up
// too early leaves a gateway running that nobody is waiting on and no workload
// is allowed to use.
const readyTimeout = 10 * time.Minute

// NewClient returns a client for the gateway control channel served on socket,
// presenting the credentials in the process environment.
func NewClient(socket string) *Client {
	return &Client{
		socket: "unix://" + socket,
	}
}

// NewClientWithCreds returns a client that presents the credentials it is
// given.
//
// qubesome mints the session's control credentials itself and keeps the client
// half in a file only it can read. Putting them in its own environment instead
// would hand them to every process it starts, workload sandboxes included, and
// the client half is what writes the map every classification decision is made
// from.
func NewClientWithCreds(socket string, ca, cert, key []byte) *Client {
	return &Client{
		socket: "unix://" + socket,
		ca:     ca,
		cert:   cert,
		key:    key,
	}
}

type Client struct {
	socket string

	// ca, cert and key are empty for a client that reads its credentials
	// from the environment.
	ca   []byte
	cert []byte
	key  []byte
}

func (c *Client) getCreds() (credentials.TransportCredentials, error) {
	caPEM, certPEM, keyPEM := c.ca, c.cert, c.key
	if len(caPEM) == 0 && len(certPEM) == 0 && len(keyPEM) == 0 {
		caPEM = []byte(os.Getenv("Q_MTLS_CA"))
		certPEM = []byte(os.Getenv("Q_MTLS_CERT"))
		keyPEM = []byte(os.Getenv("Q_MTLS_KEY"))
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load the gateway control key pair: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("failed to append the gateway control CA from PEM")
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS13,
		// The connection is made via unix socket, so there is no hostname to
		// derive the server name from. Both ends take it from the gateway's
		// own constant instead.
		ServerName: control.ServerName,
	})

	return creds, nil
}

func (c *Client) dial() (*grpc.ClientConn, error) {
	creds, err := c.getCreds()
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(c.socket, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the gateway: %w", err)
	}

	return conn, nil
}

// Register tells the gateway that address belongs to the workload named name,
// from now until it is unregistered.
//
// name is the policy key, which is EffectiveWorkload.Name. The gateway refuses
// a name its loaded policy does not mention rather than giving an address to a
// workload it has no rules for.
func (c *Client) Register(ctx context.Context, name, address string) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	slog.Debug("[gateway] calling Register", "workload", name, "address", address)
	_, err = pb.NewGatewayControlClient(conn).Register(ctx, &pb.RegisterRequest{
		Name:    name,
		Address: address,
	})
	if err != nil {
		return fmt.Errorf("failed to register workload %q: %w", name, err)
	}

	return nil
}

// Unregister tells the gateway the workload named name is gone and its address
// is to be dropped. A name the gateway does not know is not an error.
func (c *Client) Unregister(ctx context.Context, name string) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	slog.Debug("[gateway] calling Unregister", "workload", name)
	_, err = pb.NewGatewayControlClient(conn).Unregister(ctx, &pb.UnregisterRequest{Name: name})
	if err != nil {
		return fmt.Errorf("failed to unregister workload %q: %w", name, err)
	}

	return nil
}

// Reload asks the gateway to re-read the policy file it was started with.
//
// The gateway keeps its workload map across the reload, because qubesome is
// the source of truth for that map and the file says nothing about it.
//
// A gateway older than this qubesome does not serve the call and answers
// Unimplemented. That is reported as ErrReloadUnsupported so a caller can
// tell "this gateway will never re-read its policy" apart from "the reload
// was refused", which are different problems with different answers.
func (c *Client) Reload(ctx context.Context) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	slog.Debug("[gateway] calling Reload")
	_, err = pb.NewGatewayControlClient(conn).Reload(ctx, &pb.ReloadRequest{})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return ErrReloadUnsupported
		}

		return fmt.Errorf("failed to reload the gateway policy: %w", err)
	}

	return nil
}

// Ready blocks until the gateway's DNS resolver, proxy and netfilter ruleset
// are all up.
//
// The gateway holds the call open rather than answering not-ready, so this
// waits on one call instead of polling.
func (c *Client) Ready(ctx context.Context) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	slog.Debug("[gateway] calling Ready")
	_, err = pb.NewGatewayControlClient(conn).Ready(ctx, &pb.ReadyRequest{})
	if err != nil {
		return fmt.Errorf("failed to wait for the gateway to be ready: %w", err)
	}

	return nil
}
