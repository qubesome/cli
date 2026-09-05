package inception

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/flatpak"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/mtls"
	pb "github.com/qubesome/cli/pkg/inception/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
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

func (s *Server) Listen(serverCert tls.Certificate, ca []byte, socket string) error {
	lc := net.ListenConfig{}
	lis, err := lc.Listen(context.Background(), "unix", socket)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(ca) {
		return fmt.Errorf("failed to append CA from PEM")
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
		MinVersion:   tls.VersionTLS13,
		ServerName:   mtls.ProfileServerName,
	})

	gs := grpc.NewServer(grpc.Creds(creds))
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
	workload := in.GetWorkload()
	args := in.GetArgs()
	profile := s.profile.Name
	slog.Debug("[server] run-workload received", "workload", workload, "profile", profile, "args", args)

	opts := []command.Option[qubesome.Options]{
		qubesome.WithConfig(s.config),
		qubesome.WithProfile(profile),
		qubesome.WithWorkload(workload),
	}

	if err := checkRPCArgs(args); err != nil {
		return &pb.RunWorkloadReply{}, err
	}

	if len(args) > 0 {
		opts = append(opts, qubesome.WithExtraArgs(args))
	}

	err := qubesome.Run(opts...)
	return &pb.RunWorkloadReply{}, err
}

func (s *grpcServer) FlatpakRunWorkload(ctx context.Context, in *pb.FlatpakRunWorkloadRequest) (*pb.FlatpakRunWorkloadReply, error) {
	workload := in.GetWorkload()
	args := in.GetArgs()
	profile := s.profile.Name
	slog.Debug("[server] flatpak-run-workload received", "workload", workload, "profile", profile, "args", args)

	opts := []command.Option[flatpak.Options]{
		flatpak.WithConfig(s.config),
		flatpak.WithProfile(profile),
		flatpak.WithName(workload),
	}

	if err := checkRPCArgs(args); err != nil {
		return &pb.FlatpakRunWorkloadReply{}, err
	}

	if len(args) > 0 {
		opts = append(opts, flatpak.WithExtraArgs(args))
	}

	err := flatpak.Run(opts...)
	return &pb.FlatpakRunWorkloadReply{}, err
}

// checkRPCArgs rejects arguments that the target workload's command would
// read as flags.
//
// Arguments received over the inception socket come from a workload, and
// are appended to the command line of another workload in the same profile.
// A workload asks for something to be opened, it does not get to change how
// the program that opens it is run.
func checkRPCArgs(args []string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			// The caller sent something the server will not accept, so
			// say so with a status code. A plain error reaches the
			// client as codes.Unknown, which reads as a server side
			// failure and invites a retry that cannot succeed.
			return status.Errorf(codes.InvalidArgument,
				"argument %q cannot start with '-'", arg)
		}
	}

	return nil
}
