package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/template"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
)

const (
	// rootfsFile is the ext4 the machine boots. It is built from the
	// image on every launch and removed when the machine is gone.
	rootfsFile = "rootfs.ext4"

	// configFile is the machine description firecracker is started with.
	configFile = "firecracker.cfg"

	// guestCID is the machine's vsock context id. Firecracker reserves
	// everything below 3, and there is one guest behind each host socket,
	// so there is nothing to allocate and both ends can agree on a
	// constant.
	guestCID = 3
)

// bootArgs is the guest kernel's command line.
//
// root=/dev/vda rw names the ext4 BuildRootfs wrote and mounts it
// writable, because a guest's own writes go into that image and are
// discarded with it. init=/sbin/qubesome-init is the qubesome binary
// composed into the same image, and the bare word after it is the
// subcommand: the kernel passes every word it does not recognise itself,
// and that is not a key=value, to init as an argument.
//
// reboot=k panic=1 was already here and is now load-bearing rather than
// inherited. sandbox.shutdown ends a machine by asking the guest kernel
// to restart, and reboot=k is what makes that a write to the keyboard
// controller's reset line, which is what firecracker watches to know the
// machine is finished. Without it a clean shutdown is a machine that
// stays up. panic=1 covers the other ending, by rebooting a second after
// a kernel panic instead of sitting on it.
//
// pci=off is firecracker's, which has no PCI bus, and console=ttyS0 with
// keep_bootcon is where everything a guest says goes.
const bootArgs = "keep_bootcon console=ttyS0 reboot=k panic=1 pci=off " +
	"root=/dev/vda rw init=" + guestInit + " " + sandbox.VMInitCommand

// configParams is the machine firecracker is asked for.
//
// Every path in it is rendered through the template's json function,
// because a path is configuration and JSON cannot carry every character
// one may hold.
type configParams struct {
	KernelImagePath string
	BootArgs        string

	// RootFsPath is the image built from the workload's bundle, and
	// DataPath the persistent disk, which is empty when the workload
	// configured none. The two are attached with different cache types:
	// the rootfs is thrown away at shutdown so nothing it holds needs to
	// reach the host, and the data disk is the one durable thing in a
	// machine, so its flushes are honoured.
	RootFsPath string
	DataPath   string

	VCPUs     int
	MemoryMiB int

	// GuestCID and VsockPath are the two halves of the machine's vsock
	// device. Everything that reaches into the guest, the supervisor and
	// every console, crosses the socket at VsockPath.
	GuestCID  int
	VsockPath string
}

// StatePath returns where a running microVM is recorded.
//
// The key is the workload's effective name, exactly as the bwrap
// runner's is. Both write sandbox-<name>.json into the same profile
// directory and they cannot collide, because a workload names one runner
// and runs under that one.
func StatePath(ew types.EffectiveWorkload) (string, error) {
	if ew.Profile == nil {
		return "", errors.New("workload has no profile")
	}
	if err := files.ValidateName("workload name", ew.Name); err != nil {
		return "", err
	}

	return filepath.Join(files.ProfileDir(ew.Profile.Name), "sandbox-"+ew.Name+".json"), nil
}

// Run boots a workload's microVM and returns once the machine is up.
//
// It does not wait for the machine to come down, for the reason
// bwrap.Run gives: a launch typed at a terminal gives the prompt back,
// and one that arrived over the profile's socket answers the caller
// rather than holding the request open for as long as an application is
// open. What is left running here is a whole machine rather than a
// sandbox, and it is reached afterwards through its vsock socket by the
// consoles that attach to it.
func Run(ew types.EffectiveWorkload) error {
	slog.Warn("use of firecracker is experimental")

	if err := ew.Validate(); err != nil {
		return err
	}

	statePath, err := StatePath(ew)
	if err != nil {
		return err
	}

	// This is where the bwrap runner hands a second launch over to the
	// sandbox that is already up, and a machine cannot take one. A
	// machine's command is its init, so handing it a second copy of that
	// argv would start a second init inside one machine. There is
	// nothing to hand over and nothing to start, so the launch says the
	// machine is there and stops.
	//
	// types.ValidateMicroVM refuses singleInstance: false at config
	// load, so every firecracker workload reaches this.
	if sandbox.Alive(statePath) {
		slog.Info("the microVM is already running", "workload", ew.Name)

		return nil
	}

	if err := ensureDependencies(); err != nil {
		return err
	}

	in, err := resolve(ew)
	if err != nil {
		return err
	}

	cfgPath := filepath.Join(in.Dir, configFile)
	if err := writeConfig(cfgPath, in.Params); err != nil {
		return err
	}

	args := []string{"--api-sock", in.APISocket, "--config-file", cfgPath}

	slog.Debug("exec", "binary", files.FireCrackerBinary, "args", args)

	//nolint:gosec // G204: fixed binary, arguments built above.
	cmd := execabs.CommandContext(context.Background(), files.FireCrackerBinary, args...)
	// Stdin stays closed for bwrap.Run's reason. The machine keeps
	// running after the launch returns, and a detached firecracker
	// reading the serial console from the terminal the shell has taken
	// back would take the user's keystrokes. Its output is still worth
	// showing: the guest's boot messages are on it, and a machine that
	// fails to boot says so there.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the microVM: %w", err)
	}

	if err := sandbox.WriteState(statePath, cmd.Process.Pid); err != nil {
		// The state file is how a second launch finds this machine and
		// how a console knows there is one, so a machine that cannot be
		// recorded must not be left holding its memory reservation under
		// a name nothing can reach.
		if kerr := cmd.Process.Kill(); kerr != nil {
			slog.Warn("failed to kill the unrecorded microVM", "error", kerr)
		}
		_ = cmd.Wait()

		return fmt.Errorf("failed to record microVM state: %w", err)
	}

	go reap(cmd, statePath, in.Params.RootFsPath)

	return nil
}

// reap waits for a machine to come down and clears what it left behind.
//
// It runs in a goroutine because Run has already returned, and it only
// runs at all for as long as this process does. bwrap.reap describes
// what that means for the state file, and sandbox.Alive is what reads a
// stale one as not running. The root filesystem is removed here for a
// reason of its own: it is an unpacked image's worth of disk, one per
// machine, and the next boot builds a fresh one from the bundle.
func reap(cmd *execabs.Cmd, statePath, rootfs string) {
	if err := cmd.Wait(); err != nil {
		slog.Debug("the microVM exited", "path", statePath, "error", err)
	}

	remove(rootfs, "microVM root filesystem")
	remove(statePath, "microVM state")
}

func remove(path, what string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to remove the "+what, "path", path, "error", err)
	}
}

// machine is a resolved microVM, everything the host had to be asked for
// already gathered.
type machine struct {
	// Dir is the machine's runtime directory, where the config file and
	// the images it names live.
	Dir string

	// APISocket is firecracker's control socket. It is deliberately not
	// beside the vsock socket. See files.VMAPISocket.
	APISocket string

	Params configParams
}

// resolve prepares everything a machine needs before firecracker is
// started, so that Run is the start and nothing else.
func resolve(ew types.EffectiveWorkload) (machine, error) {
	wl := ew.Workload
	m := wl.MicroVM.WithDefaults()

	bundle, err := images.PullProfileImage(wl.Image)
	if err != nil {
		return machine{}, err
	}

	dir, err := files.VMRuntimeDir(ew.Profile.Name, wl.Name)
	if err != nil {
		return machine{}, err
	}

	vsockDir, err := files.VMVsockDir(ew.Profile.Name, wl.Name)
	if err != nil {
		return machine{}, err
	}

	// The vsock directory is the deeper of the two and creating it
	// creates the runtime directory above it.
	if err := os.MkdirAll(vsockDir, files.DirMode); err != nil {
		return machine{}, fmt.Errorf("failed to create the microVM runtime dir: %w", err)
	}

	apiSocket, err := files.VMAPISocket(ew.Profile.Name, wl.Name)
	if err != nil {
		return machine{}, err
	}

	vsockSocket, err := files.VMVsockSocket(ew.Profile.Name, wl.Name)
	if err != nil {
		return machine{}, err
	}

	// Firecracker binds both of these itself and refuses to start when
	// one is already there. A machine that was killed leaves them
	// behind, and this launch has already established that no machine of
	// this workload is running.
	remove(apiSocket, "stale microVM API socket")
	remove(vsockSocket, "stale microVM vsock socket")

	rootfs := filepath.Join(dir, rootfsFile)
	if err := BuildRootfs(bundle, ew, rootfs); err != nil {
		return machine{}, err
	}

	params := configParams{
		KernelImagePath: filepath.Join(files.QubesomeDir(), kernelFile),
		BootArgs:        bootArgs,
		RootFsPath:      rootfs,
		VCPUs:           m.VCPUs,
		MemoryMiB:       m.MemoryMiB,
		GuestCID:        guestCID,
		VsockPath:       vsockSocket,
	}

	if m.Data != nil {
		data, err := EnsureDataDisk(*m.Data)
		if err != nil {
			return machine{}, err
		}

		params.DataPath = data
	}

	return machine{Dir: dir, APISocket: apiSocket, Params: params}, nil
}

func writeConfig(path string, params configParams) error {
	cfg, err := renderConfig(params)
	if err != nil {
		return err
	}

	slog.Debug("writing the firecracker machine description", "path", path)

	if err := os.WriteFile(path, []byte(cfg), files.FileMode); err != nil {
		return fmt.Errorf("failed to write the firecracker machine description: %w", err)
	}

	return nil
}

// renderConfig renders the machine description firecracker is started
// with.
//
// The paths go through jsonString rather than into the template raw. A
// path comes from the configuration and JSON has characters it cannot
// carry as they are, so quoting them is what keeps a machine description
// a machine description.
func renderConfig(params configParams) (string, error) {
	t, err := template.New("config").Funcs(template.FuncMap{"json": jsonString}).Parse(configTmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse the firecracker machine description: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("failed to render the firecracker machine description: %w", err)
	}

	return buf.String(), nil
}

func jsonString(s string) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
