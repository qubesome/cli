package qubesome

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/docker"
	"github.com/qubesome/cli/internal/drive"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/firecracker"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/types"
	"gopkg.in/yaml.v3"
)

func init() { //nolint
	inception.Add("run", runCmd)
	inception.Add("xdg", xdgCmd)
}

func runCmd(cfg *types.Config, p *types.Profile, args []string) error {
	opts := []command.Option[Options]{
		WithConfig(cfg),
		WithProfile(p.Name),
		WithWorkload(args[0]),
	}

	if len(args) > 0 {
		opts = append(opts, WithExtraArgs(args[1:]))
	}

	return Run(opts...)
}

func xdgCmd(cfg *types.Config, p *types.Profile, args []string) error {
	return XdgRun(WithProfile(p.Name), WithExtraArgs(args))
}

func XdgRun(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if len(o.ExtraArgs) == 0 {
		return fmt.Errorf("xdg missing args")
	}

	if inception.Inside() {
		return inception.RunOnHost("xdg", o.ExtraArgs)
	}

	q := New()
	in := &WorkloadInfo{
		Profile: o.Profile,
		Config:  o.Config,
	}

	return q.HandleMime(in, o.ExtraArgs)
}

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if inception.Inside() {
		args := []string{o.Workload}
		args = append(args, o.ExtraArgs...)
		return inception.RunOnHost("run", args)
	}

	if err := o.Validate(); err != nil {
		return err
	}

	wg := sync.WaitGroup{}
	if err := images.Pull(o.Config, &wg); err != nil {
		return err
	}
	in := WorkloadInfo{
		Name:    o.Workload,
		Profile: o.Profile,
		Args:    o.ExtraArgs,
		Config:  o.Config,
	}

	// Wait for any background operation that is in-flight.
	defer wg.Wait()
	return runner(in)
}

func runner(in WorkloadInfo) error {
	if err := in.Validate(); err != nil {
		return err
	}

	profile, exists := in.Config.Profiles[in.Profile]
	if !exists {
		return fmt.Errorf("profile %q does not exist", in.Profile)
	}

	if len(profile.ExternalDrives) > 0 {
		slog.Debug("profile has required external drives", "drives", profile.ExternalDrives)
		for _, dm := range profile.ExternalDrives {
			split := strings.Split(dm, ":")
			if len(split) != 2 {
				return fmt.Errorf("cannot enforce external drive: invalid format")
			}

			ok, err := drive.Mounts(split[0], split[1])
			if err != nil {
				return fmt.Errorf("cannot check drive label mounts: %w", err)
			}

			if !ok {
				return fmt.Errorf("required drive %q is not mounted at %q", split[0], split[1])
			}
		}
	}

	workloadsDir, err := files.WorkloadsDir(in.Config.RootDir, profile.Path)
	if err != nil {
		return err
	}

	cfg, err := securejoin.SecureJoin(workloadsDir, fmt.Sprintf("%s.%s", in.Name, configExtension))
	if err != nil {
		return err
	}

	if fi, err := os.Stat(cfg); err != nil || fi.IsDir() {
		return fmt.Errorf("%w: %w", ErrWorkloadConfigNotFound, err)
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		return fmt.Errorf("cannot read file %q: %w", cfg, err)
	}

	w := types.Workload{}
	err = yaml.Unmarshal(data, &w)
	if err != nil {
		return fmt.Errorf("cannot unmarshal workload config %q: %w", cfg, err)
	}

	pp, err := securejoin.SecureJoin(in.Config.RootDir, profile.Path)
	if err != nil {
		return err
	}
	slog.Debug("bind workload path to profile root dir", "path", pp)
	profile.Path = pp

	if fi, err := os.Stat(profile.Path); err != nil || !fi.IsDir() {
		return fmt.Errorf("%w: %s", ErrProfileDirNotExist, profile.Path)
	}

	// TODO: find more elegant manner to auto populate profile name
	profile.Name = in.Profile
	w.Name = in.Name

	ew := w.ApplyProfile(profile)
	ew.Workload.Args = append(ew.Workload.Args, in.Args...)

	switch ew.Workload.Runner {
	case "fire-cracker":
		return firecracker.Run(ew)

	default:
		return docker.Run(ew)
	}
}
