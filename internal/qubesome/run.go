package qubesome

import (
	"fmt"
	"os"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/docker"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/firecracker"
	"github.com/qubesome/cli/internal/types"
	"gopkg.in/yaml.v3"
)

func (q *Qubesome) Run(in WorkloadInfo) error {
	if err := in.Validate(); err != nil {
		return err
	}

	workloadsDir, err := files.WorkloadsDir(in.Path)
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

	profile, exists := q.Config.Profiles[in.Profile]
	if !exists {
		return fmt.Errorf("profile %q does not exist", in.Profile)
	}

	// TODO: this should be set by caller.
	profile.Path = in.Path

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
