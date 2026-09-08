package qubesome

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/runners/docker"
	"github.com/qubesome/cli/internal/runners/firecracker"
	"github.com/qubesome/cli/internal/runners/podman"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"github.com/qubesome/cli/internal/util/drive"
	"github.com/qubesome/cli/internal/util/env"
	"go.yaml.in/yaml/v3"
)

func XdgRun(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if len(o.ExtraArgs) == 0 {
		return fmt.Errorf("xdg-open missing args")
	}

	if inception.Inside() {
		client := inception.NewClient(files.InProfileSocketPath())
		return client.XdgOpen(context.TODO(), o.ExtraArgs[0])
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
		client := inception.NewClient(files.InProfileSocketPath())
		return client.Run(context.TODO(), o.Workload, o.ExtraArgs)
	}

	if err := o.Validate(); err != nil {
		return err
	}

	in := WorkloadInfo{
		Name:    o.Workload,
		Profile: o.Profile,
		Args:    o.ExtraArgs,
		Config:  o.Config,
	}

	// Nothing config-wide happens here. Refreshing every image the
	// configuration names belongs to starting a profile, which is a
	// process that stays up, not to opening one app.
	return runner(in, o.Runner, o.Headless)
}

func runner(in WorkloadInfo, runnerOverride string, headless bool) error {
	if err := in.Validate(); err != nil {
		return err
	}

	profile, exists := in.Config.Profile(in.Profile)
	if !exists {
		return fmt.Errorf("profile %q does not exist", in.Profile)
	}

	err := env.Update("GITDIR", in.Config.RootDir)
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

	// The workload name reaches here straight from the command line or from
	// the profile's RPC, and nothing has checked it yet: the workload is
	// validated once it has been read, which is too late to decide which
	// file to read. Opening the workloads dir as a root leaves that decision
	// to the kernel, which refuses a name that walks out of it.
	root, err := os.OpenRoot(workloadsDir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrWorkloadConfigNotFound, err)
	}
	defer root.Close()

	name := fmt.Sprintf("%s.%s", in.Name, configExtension)
	cfg := filepath.Join(workloadsDir, name)

	if fi, err := root.Stat(name); err != nil || fi.IsDir() {
		return fmt.Errorf("%w: %w", ErrWorkloadConfigNotFound, err)
	}

	data, err := root.ReadFile(name)
	if err != nil {
		return fmt.Errorf("cannot read file %q: %w", cfg, err)
	}

	w := types.Workload{}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Enforces that all YAML fields match struct fields exactly.
	if err := decoder.Decode(&w); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("workload config %q is empty", cfg)
		}
		return fmt.Errorf("cannot unmarshal workload config %q: %w", cfg, err)
	}

	if filepath.IsAbs(profile.Path) {
		profile.Path, err = filepath.Rel(in.Config.RootDir, profile.Path)
		if err != nil {
			return fmt.Errorf("profile path must be relative to config rootdir: %w", err)
		}
	}

	pp, err := files.JoinRel(in.Config.RootDir, profile.Path)
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
			err := fmt.Errorf("workload %s tries to access more than profile allows", in.Name)
			dbus.NotifyOrLog("qubesome: access denied", err.Error()+":<br/>"+msg)

			return err
		}
		slog.Debug("unknown objects mismatch", "w", w, "ew", ew)
	}

	// Workloads connect to the profile's Xwayland, which is an X server
	// whatever the host session is, so the X11 arguments are the ones that
	// apply. This used to follow the host session type, which meant a
	// workload on a Wayland host was given its Wayland arguments and an X11
	// display to use them against.
	ew.Workload.Args = append(ew.Workload.Args, ew.Workload.X11Args...)

	if len(ew.Workload.WaylandArgs) > 0 {
		slog.Warn("waylandArgs are no longer applied, workloads run on the profile's X server",
			"workload", ew.Workload.Name)
	}

	if len(ew.Workload.HostAccess.Gpus) == 0 {
		ew.Workload.Args = append(ew.Workload.Args, ew.Workload.NoGPUArgs...)
	}

	ew.Workload.Args = append(ew.Workload.Args, in.Args...)

	if runnerOverride != "" {
		ew.Workload.Runner = runnerOverride
	}

	if headless {
		// In headless mode, Mime handling is not supported.
		ew.Workload.HostAccess.Mime = false
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
	if w.HostAccess.Microphone != ew.Workload.HostAccess.Microphone {
		msg = msg + "- microphone<br/>"
	}
	if w.HostAccess.Mime != ew.Workload.HostAccess.Mime {
		msg = msg + "- mime<br/>"
	}
	if w.HostAccess.SeccompUnconfined != ew.Workload.HostAccess.SeccompUnconfined {
		msg = msg + "- seccompUnconfined<br/>"
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
