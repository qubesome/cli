package bwrap

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/keyring"
	"github.com/qubesome/cli/internal/keyring/backend"
	"github.com/qubesome/cli/internal/runners/util/container"
	"github.com/qubesome/cli/internal/runners/util/mime"
	"github.com/qubesome/cli/internal/runners/util/usb"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/seccomp"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"github.com/qubesome/cli/internal/util/env"
	"github.com/qubesome/cli/internal/util/gpu"
	"golang.org/x/sys/execabs"
)

// firstExtraFD is the descriptor os/exec puts the first ExtraFiles entry
// on in the child.
const firstExtraFD = 3

// hostEnvPassthrough are the host variables a workload sharing the host
// dbus reads. The container runners named them and let the runtime copy
// the values over, and bwrap clears the environment instead.
var hostEnvPassthrough = []string{
	"DBUS_SESSION_BUS_ADDRESS",
	"XDG_RUNTIME_DIR",
	"XDG_SESSION_ID",
}

// StatePath returns where a running workload sandbox is recorded.
//
// The key is the workload's effective name, which is the name the
// container runner gave the container, so a workload keeps the same
// identity across the change.
func StatePath(ew types.EffectiveWorkload) (string, error) {
	if ew.Profile == nil {
		return "", errors.New("workload has no profile")
	}
	if err := files.ValidateName("workload name", ew.Name); err != nil {
		return "", err
	}

	return filepath.Join(files.ProfileDir(ew.Profile.Name), "sandbox-"+ew.Name+".json"), nil
}

// Run starts a workload in its own sandbox and returns once it is up.
//
// It does not wait for the workload to close, which is what docker run -d
// did and what the callers expect: qubesome run typed at a terminal gives
// the prompt back, and a launch that arrives over the profile's socket
// answers the caller rather than holding the request open for the life of
// an application. The sandbox is left running behind it, which is why the
// workload spec does not set Spec.DieWithParent.
func Run(ew types.EffectiveWorkload) error {
	if err := ew.Validate(); err != nil {
		return err
	}

	statePath, err := StatePath(ew)
	if err != nil {
		return err
	}

	// Before anything is pulled or unpacked, as the container runner
	// checked for a running container before building its argument list.
	if ew.Workload.SingleInstance {
		handled, err := handOver(ew, statePath)
		if handled || err != nil {
			return err
		}
	}

	in, err := resolve(ew)
	if err != nil {
		return err
	}

	spec, err := buildSpec(in)
	if err != nil {
		return err
	}

	var extra []*os.File
	seccompFD := -1

	if spec.Seccomp {
		filter, err := seccomp.MemFD()
		if err != nil {
			return err
		}
		defer filter.Close()

		seccompFD = firstExtraFD + len(extra)
		extra = append(extra, filter)
	}

	args, err := sandbox.Args(spec, seccompFD)
	if err != nil {
		return err
	}

	slog.Debug("exec", "binary", files.BwrapBinary, "args", container.RedactEnvArgs(args))

	// A mime enabled workload carries the profile's mTLS private key in
	// its environment, and a command line is world readable through
	// /proc. Only the descriptor holding the options, and the command,
	// stay on it.
	outer, packed, err := sandbox.PackArgs(spec, args, firstExtraFD+len(extra))
	if err != nil {
		return err
	}
	defer packed.Close()

	extra = append(extra, packed)

	cmd := execabs.Command(files.BwrapBinary, outer...) //nolint:gosec // the arguments are built from the workload config.
	cmd.ExtraFiles = extra
	// The launch returns while the workload keeps running, so stdin stays
	// closed rather than leaving a detached application reading the
	// terminal the shell has taken back. Its output is still worth
	// showing: a workload that fails to start says why there.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start workload sandbox: %w", err)
	}

	if err := sandbox.WriteState(statePath, cmd.Process.Pid); err != nil {
		// The state file is how a second launch of a single instance
		// workload finds this one, so a sandbox that cannot be recorded
		// must not keep running under a name nothing can reach.
		if kerr := cmd.Process.Kill(); kerr != nil {
			slog.Warn("failed to kill the unrecorded sandbox", "error", kerr)
		}
		_ = cmd.Wait()

		return fmt.Errorf("failed to record sandbox state: %w", err)
	}

	go reap(cmd, statePath)

	return nil
}

// reap waits for a workload sandbox to exit and clears the state file it
// was recorded in.
//
// It runs in a goroutine because Run has already returned. A launch that
// arrived over the profile's socket is served by a process that stays up,
// and there this is what keeps a closed workload from being left as a
// zombie and its state file from naming a pid that is gone. A qubesome run
// at a terminal exits long before any of that: the sandbox is reparented
// to init, which reaps it, and the state file outlives the pid it names.
// That is what sandbox.Alive is for, since it records the start time as
// well and so reads a stale file as not running.
func reap(cmd *execabs.Cmd, statePath string) {
	if err := cmd.Wait(); err != nil {
		slog.Debug("workload sandbox exited", "path", statePath, "error", err)
	}

	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to remove sandbox state", "path", statePath, "error", err)
	}
}

// handOver gives a running sandbox the command of a second launch, and
// reports whether the launch was dealt with.
//
// Two things have to hold. The state file has to name a live process, and
// the supervisor in that sandbox has to answer. Neither is enough on its
// own: the state file is a cache of a pid, and a pid that is alive says
// nothing about whether anything inside can still be reached.
//
// A live state file with a socket that does not answer is the case worth
// stating. It starts a fresh sandbox. The state file is a cache and not
// the truth, which is what a dead recorded pid already means here, and a
// sandbox nothing can be handed to is no more useful to this launch than
// one that is gone: refusing would leave the user with an application that
// does not open and a file they should not have to know about to fix it.
// The single instance guarantee is what a live supervisor gives, not a
// lock, and the trade is deliberate.
//
// A supervisor that answers and refuses is the opposite case, and it is
// reported. Something is running in there, and a second sandbox would put
// two of a single instance workload on one profile directory, which is the
// outcome the supervisor exists to prevent.
func handOver(ew types.EffectiveWorkload, statePath string) (bool, error) {
	if !sandbox.Alive(statePath) {
		return false, nil
	}

	socket, err := files.WorkloadAgentSocket(ew.Profile.Name, ew.Workload.Name)
	if err != nil {
		return false, err
	}

	argv := append([]string{ew.Workload.Command}, ew.Workload.Args...)

	err = spawn(socket, argv, statePath)
	if err == nil {
		slog.Debug("handed the workload to a running sandbox", "workload", ew.Name)
		return true, nil
	}

	if errors.Is(err, sandbox.ErrNoSupervisor) {
		slog.Warn("the recorded sandbox is not answering, starting a fresh one",
			"workload", ew.Name, "error", err)

		return false, nil
	}

	return true, fmt.Errorf("failed to hand %q to its running sandbox: %w", ew.Name, err)
}

const (
	// startupGrace bounds the wait for a sandbox that was recorded a
	// moment ago to reach the point of listening. Between the host
	// recording the sandbox and the supervisor binding its socket there is
	// a bwrap setup and an exec, and a launch that gave up inside that
	// window would start a second sandbox because the first was not quite
	// up yet.
	startupGrace = 2 * time.Second

	startupPoll = 50 * time.Millisecond
)

// spawn hands argv over, waiting for a sandbox that is still starting.
//
// Only a sandbox the state file still calls alive is waited for, so a
// sandbox that goes away during the wait ends it rather than running it
// out.
func spawn(socket string, argv []string, statePath string) error {
	deadline := time.Now().Add(startupGrace)

	for {
		err := sandbox.Spawn(socket, argv)
		if !errors.Is(err, sandbox.ErrNoSupervisor) {
			return err
		}
		if time.Now().After(deadline) || !sandbox.Alive(statePath) {
			return err
		}

		time.Sleep(startupPoll)
	}
}

// resolve gathers everything the sandbox needs from the host.
//
// Every filesystem lookup, image pull and keyring read happens here, so
// that buildSpec is a function of its input alone.
func resolve(ew types.EffectiveWorkload) (input, error) {
	wl := ew.Workload

	bundle, err := images.PullProfileImage(wl.Image)
	if err != nil {
		return input{}, err
	}

	profileDir := files.ProfileDir(ew.Profile.Name)

	userDir, err := files.IsolatedRunUserPath(ew.Profile.Name)
	if err != nil {
		return input{}, fmt.Errorf("failed to get isolated <qubesome>/user path: %w", err)
	}

	shmDir, err := files.WorkloadShmPath(ew.Profile.Name, wl.Name)
	if err != nil {
		return input{}, fmt.Errorf("failed to get workload shm path: %w", err)
	}
	if err := files.EnsureMappedDir(shmDir + string(filepath.Separator)); err != nil {
		return input{}, fmt.Errorf("failed to create workload shm dir: %w", err)
	}

	cookiePath, err := files.ClientCookiePath(ew.Profile.Name)
	if err != nil {
		return input{}, err
	}

	socketPath, err := files.SocketPath(ew.Profile.Name)
	if err != nil {
		return input{}, err
	}

	usbDevices, err := usb.NamedDevices(wl.HostAccess.USBDevices)
	if err != nil {
		return input{}, fmt.Errorf("failed to get named devices: %w", err)
	}

	in := input{
		Workload:   ew,
		Bundle:     bundle,
		ProfileDir: profileDir,
		UserDir:    userDir,
		ShmDir:     shmDir,
		CookiePath: cookiePath,
		SocketPath: socketPath,
		Localtime:  localtime(),
		USBDevices: usbDevices,
		Paths:      mappedPaths(wl.HostAccess.Paths),
	}

	if wl.HostAccess.Camera {
		in.VideoDevices, _ = filepath.Glob("/dev/video*")
	}

	if wl.HostAccess.Dbus || wl.HostAccess.Bluetooth || wl.HostAccess.VarRunUser {
		in.HostEnv = hostEnv()
	}

	if wl.HostAccess.Gpus != "" {
		nodes, mounts, err := gpu.SandboxEdits("/")
		if err != nil {
			// The container runner reported the same thing the same way,
			// and a workload without hardware rendering is still usable.
			dbus.NotifyOrLog("qubesome error", "GPU support was not detected, disabling it for qubesome")
			slog.Warn("failed to resolve GPU devices", "error", err)
		} else {
			in.GPUNodes, in.GPUMounts = nodes, mounts
		}
	}

	if needsQubesomeBin(wl) {
		// The mime handler, the supervisor and the console are all this
		// binary.
		bin, err := os.Executable()
		if err != nil {
			return input{}, err
		}
		in.QubesomeBin = bin
	}

	if wl.SingleInstance {
		agentDir, err := files.WorkloadAgentDir(ew.Profile.Name, wl.Name)
		if err != nil {
			return input{}, err
		}
		// MkdirAll rather than EnsureMappedDir: the parent is the
		// profile's agent directory, and a workload launch is the only
		// thing that creates either of them.
		if err := os.MkdirAll(agentDir, files.DirMode); err != nil {
			return input{}, fmt.Errorf("failed to create workload agent dir: %w", err)
		}
		in.AgentDir = agentDir
	}

	if wl.AttachVM != "" {
		// The machine itself was started before this, by the attach path
		// in internal/qubesome/run.go, which is also what refused an
		// attachVM naming no machine. All that is left here is where its
		// socket is.
		dir, err := files.VMVsockDir(ew.Profile.Name, wl.AttachVM)
		if err != nil {
			return input{}, err
		}
		in.VMVsockDir = dir
	}

	if wl.HostAccess.Mime {
		if err := resolveMime(&in); err != nil {
			return input{}, err
		}
	}

	return in, nil
}

// resolveMime writes the mime files the workload reads and fills in the
// paths and credentials that go with them.
func resolveMime(in *input) error {
	homeDir, err := container.HomeDir(in.Bundle)
	if err != nil {
		return err
	}
	in.HomeDir = homeDir

	if err := os.MkdirAll(in.ProfileDir, files.DirMode); err != nil {
		return fmt.Errorf("failed to ensure profile dir: %w", err)
	}

	list := filepath.Join(in.ProfileDir, "mimeapps.list")
	if err := os.WriteFile(list, []byte(mime.MimesList), files.FileMode); err != nil {
		return fmt.Errorf("failed to write mimeapps.list: %w", err)
	}

	handler := filepath.Join(in.ProfileDir, "mime-handler.desktop")
	if err := os.WriteFile(handler, []byte(mime.DefaultMimeHandler), files.FileMode); err != nil {
		return fmt.Errorf("failed to write mime-handler.desktop: %w", err)
	}

	// A workload without the credentials still starts. It simply cannot
	// reach the inception server, which is the same thing the container
	// runner did.
	if ca, cert, key, ok := mtlsData(in.Workload.Profile.Name); ok {
		slog.Debug("mime access: enabled")
		in.MTLSCA, in.MTLSCert, in.MTLSKey = ca, cert, key
	} else {
		slog.Debug("mime access: skipped")
	}

	return nil
}

// localtime returns /etc/localtime and, when it is a symlink, the file it
// points at.
//
// The link on its own resolves to nothing inside the sandbox, so both are
// shared.
func localtime() []string {
	const file = "/etc/localtime"

	if _, err := os.Stat(file); err != nil {
		return nil
	}

	paths := make([]string, 0, 2)
	paths = append(paths, file)

	target, err := os.Readlink(file)
	if err != nil {
		return paths
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(file), target)
	}

	return append(paths, target)
}

// mappedPaths expands the workload's mapped directories and creates the
// host side of each.
//
// A directory that does not exist is created here rather than by the
// sandbox, because bwrap would create it owned by the sandbox user.
//
// A mapping is src:dst with an optional :ro, which pathRegex allows a
// colon in neither half, so the fields are split rather than cut. Cutting
// at the first colon leaves the flag attached to the destination: a
// mapping ending :ro mounted onto "/home/user/.zshrc:ro" instead of
// "/home/user/.zshrc", so the file was there under a name nothing looks
// for, and read-only mappings were mounted writable.
func mappedPaths(paths []string) []sandbox.Mount {
	mounts := make([]sandbox.Mount, 0, len(paths))

	for _, p := range paths {
		parts := strings.Split(p, ":")

		src := env.Expand(parts[0])
		if err := files.EnsureMappedDir(src); err != nil {
			slog.Warn("failed to mount path", "path", src, "error", err)
			continue
		}

		m := sandbox.Mount{Src: src, Dst: src}
		if len(parts) > 1 {
			m.Dst = parts[1]
		}
		m.ReadOnly = len(parts) > 2 && parts[2] == "ro"

		mounts = append(mounts, m)
	}

	return mounts
}

// hostEnv reads the host variables a workload on the host dbus needs.
//
// A variable that is not set on the host is left out rather than passed as
// empty, which is what the container runners did with a bare -e NAME.
func hostEnv() []string {
	env := make([]string, 0, len(hostEnvPassthrough))

	for _, name := range hostEnvPassthrough {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}

	return env
}

func mtlsData(profile string) (string, string, string, bool) {
	ks := keyring.New(profile, backend.New())

	ca, err := ks.Get(keyring.MtlsCA)
	if err != nil {
		slog.Error("failed to fetch mtls-ca", "error", err)
		return "", "", "", false
	}

	cert, err := ks.Get(keyring.MtlsClientCert)
	if err != nil {
		slog.Error("failed to fetch mtls-client-cert", "error", err)
		return "", "", "", false
	}

	key, err := ks.Get(keyring.MtlsClientKey)
	if err != nil {
		slog.Error("failed to fetch mtls-client-key", "error", err)
		return "", "", "", false
	}

	return ca, cert, key, true
}
