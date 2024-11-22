package inception

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
	pb "github.com/qubesome/cli/pkg/inception/proto"
	"google.golang.org/grpc"
)

// NewServer returns a new inception server.
func NewServer(p *types.Profile, cfg *types.Config) *Server {
	return &Server{
		server: &grpcServer{
			profile: p,
			config:  cfg,
		},
	}
}

// Server represents an inception server. It is bound to a given profile,
// so all calls it receives will be constraints within that scope.
//
// Each profile can only have a single inception server.
type Server struct {
	server *grpcServer
}

func (s *Server) Listen(socket string) error {
	lis, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	gs := grpc.NewServer()
	pb.RegisterQubesomeHostServer(gs, s.server)

	slog.Debug("[server] listening", "addr", lis.Addr())
	if err := gs.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

type grpcServer struct {
	pb.UnimplementedQubesomeHostServer
	profile *types.Profile
	config  *types.Config
}

func (s *grpcServer) XdgOpen(ctx context.Context, in *pb.XdgOpenRequest) (*pb.XdgOpenReply, error) {
	url := in.GetUrl()
	profile := s.profile.Name
	slog.Debug("[server] xdg-open received", "url", url, "profile", profile)

	err := qubesome.XdgRun(
		qubesome.WithConfig(s.config),
		qubesome.WithProfile(s.profile.Name),
		qubesome.WithExtraArgs([]string{url}),
	)

	return &pb.XdgOpenReply{}, err
}

func (s *grpcServer) RunWorkload(ctx context.Context, in *pb.RunWorkloadRequest) (*pb.RunWorkloadReply, error) {
	worload := in.GetWorkload()
	args := in.GetArgs()
	profile := s.profile.Name
	slog.Debug("[server] run-workload received", "workload", worload, "profile", profile, "args", args)

	opts := []command.Option[qubesome.Options]{
		qubesome.WithConfig(s.config),
		qubesome.WithProfile(profile),
		qubesome.WithWorkload(worload),
	}

	if len(args) > 0 {
		opts = append(opts, qubesome.WithExtraArgs(strings.Split(args, " ")))
	}

	err := qubesome.Run(opts...)
	return &pb.RunWorkloadReply{}, err
}
