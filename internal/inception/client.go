package inception

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pb "github.com/qubesome/cli/pkg/inception/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewClient(socket string) *Client {
	return &Client{
		socket: "unix://" + socket,
	}
}

type Client struct {
	socket string
}

func (c *Client) XdgOpen(ctx context.Context, url string) error {
	conn, err := grpc.NewClient(c.socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	conn, err := grpc.NewClient(c.socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
