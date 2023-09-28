package qubesome

import (
	"fmt"
	"os"
	"path/filepath"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/qubesome-cli/internal/config"
	"github.com/qubesome/qubesome-cli/internal/docker"
	"github.com/qubesome/qubesome-cli/internal/firecracker"
	"github.com/qubesome/qubesome-cli/internal/workload"
	"gopkg.in/yaml.v3"
)

func (q *Qubesome) Run(in WorkloadInfo) error {
	if err := in.Validate(); err != nil {
		return err
	}

	qh, err := qubesomeHome()
	if err != nil {
		return err
	}

	profileDir, err := securejoin.SecureJoin(qh, filepath.Join(profilesDir, in.Profile))
	if err != nil {
		return err
	}

	if fi, err := os.Stat(profileDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%w: %s", ErrProfileDirNotExist, profileDir)
	}

	cfg, err := securejoin.SecureJoin(qh, filepath.Join(workloadsDir, fmt.Sprintf("%s-%s.%s", in.Name, in.Profile, configExtension)))
	if err != nil {
		return err
	}

	if fi, err := os.Stat(cfg); err != nil || fi.IsDir() {
		return fmt.Errorf("%w: %w", ErrWorkloadConfigNotFound, err)
	}

	//TODO: limit reader
	data, err := os.ReadFile(cfg)
	if err != nil {
		return fmt.Errorf("cannot read file %q: %w", cfg, err)
	}

	wlDefault := config.Workload{}
	err = yaml.Unmarshal(data, &wlDefault)
	if err != nil {
		return fmt.Errorf("cannot unmarshal workload config %q: %w", cfg, err)
	}

	wl := workload.Effective{
		Profile: in.Profile,

		Name:    in.Name,
		Image:   wlDefault.Image,
		Command: wlDefault.Command,
		Args:    wlDefault.Args,
		Opts: workload.Opts{
			Camera:     wlDefault.HostAccess.Camera,    // TODO: pick up from named device from profile
			SmartCard:  wlDefault.HostAccess.Smartcard, // TODO: pick up from named device from profile
			Microphone: wlDefault.HostAccess.Microphone,
			Speakers:   wlDefault.HostAccess.Speakers,
			X11:        wlDefault.HostAccess.X11,
			Network:    wlDefault.HostAccess.Network,
			VarRunUser: wlDefault.HostAccess.VarRunUser,
		},
		SingleInstance: wlDefault.SingleInstance,
		Path:           wlDefault.Paths,
		NamedDevices:   wlDefault.NamedDevices,
		Runner:         wlDefault.Runner,
	}

	switch wl.Runner {
	case "microvm":
		return firecracker.Run(wl)

	default:
		return docker.Run(wl)
	}
}
