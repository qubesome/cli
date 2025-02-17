package inception

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/qubesome/cli/internal/util/mtls"
	pb "github.com/qubesome/cli/pkg/inception/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func NewClient(socket string) *Client {
	return &Client{
		socket: "unix://" + socket,
	}
}

type Client struct {
	socket string
}

func getCreds() (credentials.TransportCredentials, error) {
	caPEM := []byte(os.Getenv("Q_MTLS_CA"))
	certPEM := []byte(os.Getenv("Q_MTLS_CERT"))
	keyPEM := []byte(os.Getenv("Q_MTLS_KEY"))

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caPEM) {
		return nil, err
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS13,
		// The connection is made via unix socket, so generally the
		// expected server name will be localhost - unless overridden
		// by ServerName.
		ServerName: mtls.HostServerName,
	})

	return creds, nil
}

func (c *Client) XdgOpen(ctx context.Context, url string) error {
	creds, err := getCreds()
	if err != nil {
		return err
	}

	conn, err := grpc.NewClient(c.socket,
		grpc.WithTransportCredentials(creds))
	if err != nil {
		return fmt.Errorf("failed to connect to qubesome host: %w", err)
	}
	defer conn.Close()

	cl := pb.NewQubesomeHostClient(conn)

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	slog.Debug("[client] calling XdgOpen", "url", url)
	_, err = cl.XdgOpen(ctx, &pb.XdgOpenRequest{Url: url})
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) Run(ctx context.Context, workload string, args []string) error {
	creds, err := getCreds()
	if err != nil {
		return err
	}

	conn, err := grpc.NewClient(c.socket, grpc.WithTransportCredentials(creds))
	if err != nil {
		return fmt.Errorf("failed to connect to qubesome host: %w", err)
	}
	defer conn.Close()

	cl := pb.NewQubesomeHostClient(conn)

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	slog.Debug("[client] calling RunWorkload", "workload", workload, "args", args)
	_, err = cl.RunWorkload(ctx, &pb.RunWorkloadRequest{
		Workload: workload,
		Args:     strings.Join(args, " "),
	})
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) FlatpakRun(ctx context.Context, workload string, args []string) error {
	creds, err := getCreds()
	if err != nil {
		return err
	}

	conn, err := grpc.NewClient(c.socket, grpc.WithTransportCredentials(creds))
	if err != nil {
		return fmt.Errorf("failed to connect to qubesome host: %w", err)
	}
	defer conn.Close()

	cl := pb.NewQubesomeHostClient(conn)

	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	slog.Debug("[client] calling FlatpakRunWorkload", "workload", workload, "args", args)
	_, err = cl.FlatpakRunWorkload(ctx, &pb.FlatpakRunWorkloadRequest{
		Workload: workload,
		Args:     strings.Join(args, " "),
	})
	if err != nil {
		return err
	}

	return nil
}
