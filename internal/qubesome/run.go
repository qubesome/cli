package qubesome

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/drive"
	"github.com/qubesome/cli/internal/env"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/runners/docker"
	"github.com/qubesome/cli/internal/runners/firecracker"
	"github.com/qubesome/cli/internal/runners/podman"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"gopkg.in/yaml.v3"
)

func init() { //nolint
	inception.Add("run", runCmd)
	inception.Add("xdg-open", xdgCmd)
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
	return XdgRun(WithConfig(cfg), WithProfile(p.Name), WithExtraArgs(args))
}

func XdgRun(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if len(o.ExtraArgs) == 0 {
		return fmt.Errorf("xdg-open missing args")
	}

	if inception.Inside() {
		return inception.RunOnHost("xdg-open", o.ExtraArgs)
	}

	q := New()
	in := &WorkloadInfo{
		Profile: o.Profile,
		Config:  o.Config,
	}

	return q.HandleMime(in, o.ExtraArgs, o.Runner)
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
	bin := files.ContainerRunnerBinary(o.Runner)
	if err := images.Pull(bin, o.Config, &wg); err != nil {
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
	return runner(in, o.Runner)
}

func runner(in WorkloadInfo, runnerOverride string) error {
	if err := in.Validate(); err != nil {
		return err
	}

	profile, exists := in.Config.Profiles[in.Profile]
	if !exists {
		return fmt.Errorf("profile %q does not exist", in.Profile)
	}

	path := files.ProfileConfig(in.Profile)
	target, err := os.Readlink(path)
	if err != nil {
		slog.Debug("not able find profile path", "path", path, "error", err)
		return nil
	}

	gitdir := filepath.Dir(filepath.Dir(target))
	err = env.Update("GITDIR", gitdir)
	if err != nil {
		return err
	}

	// TODO: Add tests/validation on profile format.
	if len(profile.ExternalDrives) > 0 {
		slog.Debug("profile has required external drives", "drives", profile.ExternalDrives)
		for _, dm := range profile.ExternalDrives {
			split := strings.Split(dm, ":")
			if len(split) != 3 {
				return fmt.Errorf("cannot enforce external drive: invalid format")
			}

			label := split[0]
			ok, err := drive.Mounts(split[1], split[2])
			if err != nil {
				return fmt.Errorf("cannot check drive label mounts: %w", err)
			}

			if !ok {
				return fmt.Errorf("required drive %q is not mounted at %q", split[0], split[1])
			}

			env.Add(label, split[2])
		}
	}

	var workloadsDir string
	rel, err := filepath.Rel(in.Config.RootDir, profile.Path)
	if err != nil {
		workloadsDir, err = files.WorkloadsDir(in.Config.RootDir, profile.Path)
	} else {
		workloadsDir, err = files.WorkloadsDir(in.Config.RootDir, rel)
	}
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

	if filepath.IsAbs(profile.Path) {
		profile.Path, err = filepath.Rel(in.Config.RootDir, profile.Path)
		if err != nil {
			return fmt.Errorf("profile path must be relative to config rootdir: %w", err)
		}
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
	if !reflect.DeepEqual(ew.Workload.HostAccess, w.HostAccess) {
		msg := diffMessage(w, ew)
		if len(msg) > 0 {
			err := fmt.Errorf("workload %s tries to access more than profile allows", in.Profile)
			dbus.NotifyOrLog("qubesome: access denied", err.Error()+":<br/>"+msg)

			return err
		}
		slog.Debug("unknown objects mismatch", "w", w, "ew", ew)
	}

	ew.Workload.Args = append(ew.Workload.Args, in.Args...)

	if runnerOverride != "" {
		ew.Workload.Runner = runnerOverride
	}

	switch ew.Workload.Runner {
	case "firecracker":
		return firecracker.Run(ew)
	case "podman":
		return podman.Run(ew)

	default:
		return docker.Run(ew)
	}
}

func diffMessage(w types.Workload, ew types.EffectiveWorkload) string {
	var msg string
	if w.HostAccess.Bluetooth != ew.Workload.HostAccess.Bluetooth {
		msg = msg + "- bluetooth<br/>"
	}
	if w.HostAccess.Camera != ew.Workload.HostAccess.Camera {
		msg = msg + "- camera<br/>"
	}
	if w.HostAccess.Mime != ew.Workload.HostAccess.Mime {
		msg = msg + "- mime<br/>"
	}
	if w.HostAccess.Privileged != ew.Workload.HostAccess.Privileged {
		msg = msg + "- privileged<br/>"
	}
	if w.HostAccess.Speakers != ew.Workload.HostAccess.Speakers {
		msg = msg + "- speakers<br/>"
	}
	if w.HostAccess.VarRunUser != ew.Workload.HostAccess.VarRunUser {
		msg = msg + "- VarRunUser<br/>"
	}
	if w.HostAccess.Dbus != ew.Workload.HostAccess.Dbus {
		msg = msg + "- Dbus<br/>"
	}
	if w.HostAccess.Gpus != ew.Workload.HostAccess.Gpus {
		msg = msg + "- gpus: " + w.HostAccess.Gpus + "<br/>"
	}
	if w.HostAccess.Network != ew.Workload.HostAccess.Network {
		msg = msg + "- network: " + w.HostAccess.Network + "<br/>"
	}
	if !reflect.DeepEqual(w.HostAccess.Paths, ew.Workload.HostAccess.Paths) {
		msg = msg + "- Paths<br/>"
		for _, paths := range w.HostAccess.Paths {
			msg = msg + "  - " + paths + "<br/>"
		}
	}
	if !reflect.DeepEqual(w.HostAccess.USBDevices, ew.Workload.HostAccess.USBDevices) {
		msg = msg + "- USBDevices:<br/>"
		for _, usb := range w.HostAccess.USBDevices {
			msg = msg + "  - " + usb + "<br/>"
		}
	}
	if !reflect.DeepEqual(w.HostAccess.Devices, ew.Workload.HostAccess.Devices) {
		msg = msg + "- Devices requested:<br/>"
		for _, dev := range w.HostAccess.Devices {
			msg = msg + "  - " + dev + "<br/>"
		}
	}
	if len(w.HostAccess.CapsAdd) > 0 &&
		!reflect.DeepEqual(w.HostAccess.CapsAdd, ew.Workload.HostAccess.CapsAdd) {
		msg = msg + "- CapsAdd:<br/>"
		for _, cap := range w.HostAccess.CapsAdd {
			msg = msg + "  - " + cap + "<br/>"
		}
	}
	return msg
}
