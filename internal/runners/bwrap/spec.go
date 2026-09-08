// Package bwrap builds the sandbox a workload runs in.
//
// It is the workload half of what internal/profiles does for a profile:
// it turns an EffectiveWorkload into a sandbox.Spec and hands it to
// sandbox.Args. Everything it knows about the host is gathered by Run and
// passed in through input, so building a spec touches no filesystem and no
// keyring, and the rendered arguments can be compared against a golden
// file.
package bwrap

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/gpu"
)

// runUserDir is the runtime directory a workload sees. Every workload runs
// as uid 1000 inside its own user namespace, so the path is fixed whatever
// the host uid is.
const runUserDir = "/run/user/1000"

// input carries everything a workload sandbox needs that comes from the
// host rather than from the workload definition.
//
// It exists so that the host lookups all happen in Run, and buildSpec
// stays a pure function of its arguments.
type input struct {
	Workload types.EffectiveWorkload

	// Bundle is the unpacked workload image. Its rootfs is the sandbox's
	// lower layer, and its environment and working directory are applied
	// by hand, since bwrap reads neither from an image.
	Bundle images.Bundle

	// ProfileDir is the profile's directory on the host, holding the
	// generated machine-id and the mime files written for this launch.
	ProfileDir string

	// UserDir is the profile's isolated runtime directory, shared with
	// every workload that is not on the host dbus.
	UserDir string

	// ShmDir is this workload's own /dev/shm directory. It is per
	// workload, so shared memory is not a channel between siblings.
	ShmDir string

	// CookiePath is the X client cookie, shared as the workload's
	// .Xauthority.
	CookiePath string

	// SocketPath is the profile's qube.sock, shared with a workload that
	// handles mime types so it can call back to the host.
	SocketPath string

	// QubesomeBin is the qubesome binary on the host. A mime enabled
	// workload runs it as the handler, and a single instance workload runs
	// it as its entrypoint.
	QubesomeBin string

	// AgentDir is this workload's supervisor socket directory on the host.
	// It is bound into the sandbox, where the supervisor creates the
	// socket. Set only for a single instance workload.
	AgentDir string

	// HomeDir is the home directory of the image's user, which is where a
	// mime enabled workload's desktop files have to land. Empty when the
	// workload does not handle mime types.
	HomeDir string

	// Localtime is /etc/localtime and, when that is a symlink, the file it
	// points at. Both are needed: the link alone resolves to nothing
	// inside the sandbox.
	Localtime []string

	// VideoDevices are the /dev/video* nodes found on the host.
	VideoDevices []string

	// USBDevices are the device nodes of the USB devices the workload
	// named, from usb.NamedDevices.
	USBDevices []string

	// GPUNodes and GPUMounts come from gpu.SandboxEdits. Both are empty
	// when the workload asks for no GPU, or when the host has none to
	// give.
	GPUNodes  []gpu.DeviceNode
	GPUMounts []gpu.Mount

	// Paths are the workload's mapped directories, already expanded and
	// created on the host.
	Paths []sandbox.Mount

	// HostEnv holds the host variables a workload on the host dbus reads.
	// The container runners named them and let the runtime copy the
	// values across. bwrap clears the environment instead, so the values
	// are resolved before they get here.
	HostEnv []string

	// MTLSCA, MTLSCert and MTLSKey are the profile's client credentials,
	// set only for a mime enabled workload. They reach the sandbox
	// through Spec.Env, which PackArgs keeps off the command line.
	MTLSCA   string
	MTLSCert string
	MTLSKey  string
}

// buildSpec renders a workload into the sandbox it runs in.
func buildSpec(in input) (sandbox.Spec, error) {
	wl := in.Workload.Workload

	if wl.Command == "" {
		// A container runner fell back to the image's entrypoint. bwrap
		// has no such fallback, and the unpacked bundle does not carry
		// one either, so there is nothing to run.
		return sandbox.Spec{}, fmt.Errorf("workload %q has no command", in.Workload.Name)
	}
	if in.Workload.Profile == nil {
		return sandbox.Spec{}, errors.New("workload has no profile")
	}
	if wl.SingleInstance && (in.QubesomeBin == "" || in.AgentDir == "") {
		// Without both, the sandbox would run the workload directly and
		// answer nothing, so a second launch would start a second sandbox
		// against the same data.
		return sandbox.Spec{}, fmt.Errorf(
			"workload %q is single instance but has no supervisor binary or socket dir", in.Workload.Name)
	}

	devices, err := workloadDevices(in)
	if err != nil {
		return sandbox.Spec{}, err
	}

	caps, err := capsAdd(wl.HostAccess.CapsAdd)
	if err != nil {
		return sandbox.Spec{}, err
	}

	spec := sandbox.Spec{
		Rootfs: in.Bundle.Rootfs,

		// The workload's name already carries the profile, and it is the
		// same string that keys its sandbox state file.
		Hostname: in.Workload.Name,

		UID: in.Bundle.UID,
		GID: in.Bundle.GID,

		Net: workloadNet(wl.HostAccess.Network),

		Seccomp: !wl.HostAccess.SeccompUnconfined,

		// DisableUserns is deliberately left off. Chromium builds a user
		// namespace for its own sandbox, and most workloads are browsers
		// or ship one.
		//
		// RuntimeDir is left empty for a different reason: the runtime
		// directory is always a bind mount from the host, either the
		// profile's isolated one or the host's own, so there is nothing
		// for bwrap to create.

		CapsAdd: caps,
		Devices: devices,
		Mounts:  workloadMounts(in),
		Env:     workloadEnv(in),
		Args:    workloadArgs(in),
		Cwd:     in.Bundle.Cwd,
	}

	// A workload may run as a different user from the one the image
	// declares, and inside its own user namespace only one uid and one gid
	// are mapped, so the two move together.
	if wl.User != nil {
		spec.UID = *wl.User
		spec.GID = *wl.User
	}

	return spec, nil
}

// workloadNet maps the workload's network grant onto the sandbox.
//
// host is the one grant a sandbox can honour today, and it is honoured
// because the alternative is a workload that was given the host network
// and silently got an empty namespace instead.
//
// Everything else, a named network included, gets an empty namespace with
// loopback and nothing else. The uplink lives in the gateway, which is a
// later stage, so there is deliberately no egress for those. The name is
// not refused here. types.WarnIgnoredNetwork reports it once per launch.
func workloadNet(network string) sandbox.NetMode {
	if network == "host" {
		return sandbox.NetHost
	}

	return sandbox.NetNone
}

// workloadArgs is what the sandbox runs.
//
// A single instance workload runs the supervisor, which runs the
// workload's own command and then answers on a socket. The container
// runner re-entered a running container with docker exec, and a sandbox
// cannot be entered at all, so the second launch of one of these is handed
// to a process that is already inside.
func workloadArgs(in input) []string {
	wl := in.Workload.Workload

	args := append([]string{wl.Command}, wl.Args...)
	if !wl.SingleInstance {
		return args
	}

	return append([]string{files.InProfileBinary, sandbox.SuperviseCommand}, args...)
}

// capsAdd renders the docker spelling the configuration carries into the
// one bwrap accepts.
//
// bwrap rejects a bare NET_ADMIN with "unknown cap", which at least fails
// loudly, but it fails at launch rather than here.
func capsAdd(caps []string) ([]string, error) {
	if len(caps) == 0 {
		return nil, nil
	}

	out := make([]string, 0, len(caps))
	for _, c := range caps {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c == "" {
			return nil, errors.New("capsAdd holds an empty capability")
		}
		if !strings.HasPrefix(c, "CAP_") {
			c = "CAP_" + c
		}
		out = append(out, c)
	}

	return out, nil
}

// workloadDevices lists the host device nodes shared with the workload.
//
// Each becomes a --dev-bind of the host node onto the same path, which is
// a bind of the same inode, so the node keeps the host ACLs granting the
// logged in user access to it.
func workloadDevices(in input) ([]string, error) {
	wl := in.Workload.Workload

	// /dev/dri comes first and the individual nodes follow. A bind of a
	// directory hides anything bound inside it earlier, so the render
	// nodes a GPU workload needs would disappear under it in the other
	// order.
	devices := []string{"/dev/dri"}

	for _, n := range in.GPUNodes {
		devices = append(devices, n.Path)
	}

	for _, d := range wl.HostAccess.Devices {
		src, dst, perms, err := types.ParseDevice(d)
		if err != nil {
			return nil, err
		}

		// A container runner recreated the node at dst under a cgroup
		// rule spelled by perms. A bind mount does neither: it puts the
		// host node where it is asked to, with the access the host node
		// already grants. Rather than quietly granting more than was
		// asked for, both are refused.
		if dst != src {
			return nil, fmt.Errorf("device %q: the sandbox cannot remap a device node", d)
		}
		if perms != "rwm" {
			return nil, fmt.Errorf("device %q: the sandbox cannot restrict device permissions", d)
		}

		devices = append(devices, src)
	}

	if wl.HostAccess.Microphone || wl.HostAccess.Speakers {
		devices = append(devices, "/dev/snd")
	}

	if wl.HostAccess.Camera {
		// The video group is gone with the container runner. bwrap maps a
		// single uid and gid, so a supplementary group does not survive
		// into the sandbox. The uaccess ACL on a camera node names the
		// logged in user and is evaluated against the fsuid, which does.
		devices = append(devices, in.VideoDevices...)
	}

	// One bind is enough for a USB device, where the container runner
	// needed two. It used --device to grant the device cgroup rule and
	// recreate the node, and a bind mount on top to bring the host inode
	// and with it the ACLs a security key relies on. --dev-bind is that
	// bind, and it lifts the device restriction in the same step, so the
	// node inside is the host node with the host ACLs. Only the nodes of
	// the requested devices are shared, never the tree they live in.
	devices = append(devices, in.USBDevices...)

	return dedupe(devices), nil
}

// workloadMounts lists the host paths shared with the workload, in the
// order they are applied. A later mount lands on top of an earlier one.
func workloadMounts(in input) []sandbox.Mount {
	wl := in.Workload.Workload
	profile := in.Workload.Profile

	var mounts []sandbox.Mount

	for _, p := range in.Localtime {
		mounts = append(mounts, sandbox.Mount{Src: p, Dst: p, ReadOnly: true})
	}

	mounts = append(mounts, sandbox.Mount{Src: in.ShmDir, Dst: "/dev/shm"})

	if wl.HostAccess.Dbus || wl.HostAccess.Bluetooth || wl.HostAccess.VarRunUser {
		// The whole host runtime directory is shared, so there is no
		// point mounting the descending dbus paths on top of it.
		mounts = append(mounts,
			sandbox.Mount{Src: runUserDir, Dst: runUserDir},
			sandbox.Mount{Src: "/run/dbus/system_bus_socket", Dst: "/run/dbus/system_bus_socket"},
			sandbox.Mount{Src: "/var/lib/dbus", Dst: "/var/lib/dbus"},
			sandbox.Mount{Src: "/usr/share/dbus-1", Dst: "/usr/share/dbus-1"},
			sandbox.Mount{Src: "/etc/machine-id", Dst: "/etc/machine-id", ReadOnly: true},
		)
	} else {
		mounts = append(mounts,
			sandbox.Mount{Src: in.UserDir, Dst: runUserDir},
			sandbox.Mount{
				Src:      filepath.Join(in.ProfileDir, "machine-id"),
				Dst:      "/etc/machine-id",
				ReadOnly: true,
			},
		)
	}

	if wl.HostAccess.Microphone || wl.HostAccess.Speakers {
		mounts = append(mounts, sandbox.Mount{
			Src: filepath.Join(runUserDir, "pipewire-0"),
			Dst: filepath.Join(runUserDir, "pipewire-0"),
		})
	}

	x11Socket := fmt.Sprintf("/tmp/.X11-unix/X%d", profile.Display)
	mounts = append(mounts,
		sandbox.Mount{Src: in.CookiePath, Dst: "/tmp/.Xauthority", ReadOnly: true},
		sandbox.Mount{Src: x11Socket, Dst: x11Socket},
	)

	if wl.HostAccess.Mime {
		apps := filepath.Join(in.HomeDir, ".local", "share", "applications")

		mounts = append(mounts,
			sandbox.Mount{
				Src:      filepath.Join(in.ProfileDir, "mimeapps.list"),
				Dst:      filepath.Join(apps, "mimeapps.list"),
				ReadOnly: true,
			},
			sandbox.Mount{
				Src:      filepath.Join(in.ProfileDir, "mime-handler.desktop"),
				Dst:      filepath.Join(apps, "qubesome-default-handler.desktop"),
				ReadOnly: true,
			},
			sandbox.Mount{Src: in.SocketPath, Dst: files.InProfileSocketPath(), ReadOnly: true},
		)
	}

	// Both the mime handler and the supervisor are the qubesome binary, so
	// a workload that is both still shares it once.
	if wl.HostAccess.Mime || wl.SingleInstance {
		mounts = append(mounts, sandbox.Mount{
			Src: in.QubesomeBin, Dst: files.InProfileBinary, ReadOnly: true,
		})
	}

	if wl.SingleInstance {
		// The directory rather than the socket: the socket does not exist
		// yet, the supervisor inside creates it. Writable for the same
		// reason.
		mounts = append(mounts, sandbox.Mount{
			Src: in.AgentDir, Dst: files.InWorkloadAgentDir(),
		})
	}

	for _, m := range in.GPUMounts {
		mounts = append(mounts, sandbox.Mount{
			Src:      m.HostPath,
			Dst:      m.ContainerPath,
			ReadOnly: true,
		})
	}

	return append(mounts, in.Paths...)
}

// workloadEnv builds the whole environment of the workload process.
//
// bwrap starts from nothing, so the image's own environment is part of
// this list rather than something the runtime applies underneath it.
func workloadEnv(in input) []string {
	wl := in.Workload.Workload
	profile := in.Workload.Profile

	const extra = 8

	env := make([]string, 0, len(in.Bundle.Env)+len(in.HostEnv)+extra)
	env = append(env, in.Bundle.Env...)
	env = append(env,
		"DISPLAY=:"+strconv.Itoa(int(profile.Display)),
		"XAUTHORITY=/tmp/.Xauthority",
		"QUBESOME_PROFILE="+profile.Name,
	)

	if profile.Timezone != "" {
		env = append(env, "TZ="+profile.Timezone)
	}

	env = append(env, in.HostEnv...)

	if wl.HostAccess.Mime {
		// A mime enabled workload calls back into the inception server,
		// which asks for a client certificate.
		env = append(env,
			"Q_MTLS_CA="+in.MTLSCA,
			"Q_MTLS_CERT="+in.MTLSCert,
			"Q_MTLS_KEY="+in.MTLSKey,
		)
	}

	return env
}

// dedupe drops repeated entries, keeping the first of each.
//
// The GPU nodes and the render nodes under /dev/dri overlap, and binding
// the same path twice is only noise in the arguments.
func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}

	return out
}
