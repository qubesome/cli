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
	"time"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/inception"
	"github.com/qubesome/cli/internal/runners/bwrap"
	"github.com/qubesome/cli/internal/runners/firecracker"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"github.com/qubesome/cli/internal/util/drive"
	"github.com/qubesome/cli/internal/util/env"
	"go.yaml.in/yaml/v3"
)

// firecrackerRunner is the runner that backs a workload with a microVM.
const firecrackerRunner = "firecracker"

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

	workloadsDir, err := files.WorkloadsDir(in.Config.RootDir, profile.Path)
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

	w, err := readWorkload(root, in.Name)
	if err != nil {
		return err
	}

	pp, err := files.JoinProfilePath(in.Config.RootDir, profile.Path)
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

	// The effective value, so a name inherited from the profile is
	// reported once here rather than at every validation of it.
	types.WarnIgnoredNetwork(ew.Name, ew.Workload.HostAccess.Network, in.Config.Gateway != nil)
	types.WarnIgnoredMicroVMFields(ew.Name, ew.Workload)

	// A workload that could renumber its own interface could claim another
	// workload's policy and another workload's injected credentials, so the
	// gateway cannot give one an address. It is refused here rather than at
	// launch because it is a fact about the configuration, and because the
	// same workload used to work: the message has to say what to change.
	if err := in.Config.ValidateGatewayAccess(ew); err != nil {
		return err
	}

	if ew.Workload.AttachVM != "" {
		if err := attachVM(root, profile, ew.Workload.AttachVM); err != nil {
			return err
		}
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

	// Every branch is named. Workloads run under bwrap, and the only
	// alternative left is firecracker, which keeps its own path. A runner
	// this does not know about is refused rather than sent to bwrap: a
	// configuration that asked for a different runtime and silently got
	// this one is the failure worth avoiding.
	switch ew.Workload.Runner {
	case "":
		// The config comes with the workload because this is the branch
		// that can be given a gateway address. A configured gateway that
		// will not start stops the launch, which is the one outcome this
		// stage exists to guarantee, and a configuration with no gateway
		// block asks for no egress and is unaffected.
		//
		// A microVM is handed none of it. It has a network stack of its
		// own and takes no address from the gateway, so starting one for
		// it would be a process nothing was going to talk to.
		return bwrap.Run(ew, in.Config)
	case firecrackerRunner:
		return firecracker.Run(ew)
	case "docker", "podman":
		return fmt.Errorf("workload %q asks for the %q runner, which has been removed: workloads run under bwrap",
			in.Name, ew.Workload.Runner)
	default:
		return fmt.Errorf("workload %q asks for an unknown runner %q", in.Name, ew.Workload.Runner)
	}
}

// readWorkload reads and decodes one workload of a profile.
//
// root is the profile's workloads directory, already opened, so the
// kernel refuses a name that walks out of it. See the comment on the
// OpenRoot in runner for why the read is the check.
//
// It is a function of its own because the attach path reads a second
// workload from the same directory, and the two must read a workload the
// same way. KnownFields in particular: a target whose config carries a
// field qubesome does not have is refused there as it is here, rather
// than being booted with the field ignored.
func readWorkload(root *os.Root, name string) (types.Workload, error) {
	file := fmt.Sprintf("%s.%s", name, configExtension)
	cfg := filepath.Join(root.Name(), file)

	if fi, err := root.Stat(file); err != nil || fi.IsDir() {
		return types.Workload{}, fmt.Errorf("%w: %w", ErrWorkloadConfigNotFound, err)
	}

	data, err := root.ReadFile(file)
	if err != nil {
		return types.Workload{}, fmt.Errorf("cannot read file %q: %w", cfg, err)
	}

	w := types.Workload{}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Enforces that all YAML fields match struct fields exactly.
	if err := decoder.Decode(&w); err != nil {
		if errors.Is(err, io.EOF) {
			return types.Workload{}, fmt.Errorf("workload config %q is empty", cfg)
		}
		return types.Workload{}, fmt.Errorf("cannot unmarshal workload config %q: %w", cfg, err)
	}

	return w, nil
}

const (
	// vmSocketGrace bounds the wait for a machine that was just started
	// to have a socket to be reached on. Firecracker binds it while it
	// is reading the machine description, which is before the guest
	// kernel is even loaded, so anything slower than this is a
	// firecracker that did not start rather than one that is starting.
	vmSocketGrace = 10 * time.Second

	vmSocketPoll = 50 * time.Millisecond
)

// attachVM starts the machine a workload attaches to and waits for it to
// be reachable.
//
// The refusals here are the one part of the microVM configuration that
// cannot be answered at config load. types.ValidateMicroVM sees a single
// workload file and what attachVM names lives in another one, so whether
// the target exists, and whether it is a firecracker workload, can only
// be answered where the other file can be read. That is here.
//
// The name has been through files.ValidateName by the time it arrives,
// which is what makes it safe to build a filename from, and root is the
// profile's workloads directory, so the kernel refuses one that walks
// out of it anyway.
//
// Starting the machine is a no-op when it is already up, which is what
// makes a second console on one machine the ordinary case rather than a
// second machine.
func attachVM(root *os.Root, profile *types.Profile, name string) error {
	w, err := readWorkload(root, name)
	if err != nil {
		// A file that is not there and a file that does not decode are
		// different mistakes in the configuration, and the name of a
		// workload that does exist is the wrong thing to go looking for.
		if errors.Is(err, ErrWorkloadConfigNotFound) {
			return fmt.Errorf("attachVM names %q, which is not a workload of this profile: %w", name, err)
		}

		return fmt.Errorf("attachVM names %q, whose config cannot be read: %w", name, err)
	}

	if w.Runner != firecrackerRunner {
		return fmt.Errorf("attachVM names %q, which is not a firecracker workload: there is no machine to attach to", name)
	}

	w.Name = name

	if err := firecracker.Run(w.ApplyProfile(profile)); err != nil {
		return fmt.Errorf("failed to start the microVM %q: %w", name, err)
	}

	socket, err := files.VMVsockSocket(profile.Name, name)
	if err != nil {
		return err
	}

	return waitForVMSocket(socket)
}

// waitForVMSocket waits for firecracker to bind the socket a console is
// reached over.
//
// The machine is started and the console workload is built right after
// it, so without this the console would dial a path that firecracker has
// not created yet and report a machine that is not there.
func waitForVMSocket(path string) error {
	deadline := time.Now().Add(vmSocketGrace)

	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("the microVM did not open %q within %s", path, vmSocketGrace)
		}

		time.Sleep(vmSocketPoll)
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
