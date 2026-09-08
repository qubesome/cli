package bwrap

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/gpu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seccompFD is the descriptor the filter takes in a launch where it is the
// only extra file, which is every launch with seccomp on.
const seccompFD = 3

// golden compares args against testdata/<name>.golden, one argument per
// line. Regenerate with: go test ./internal/runners/bwrap/... -update
func golden(t *testing.T, name string, args []string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	got := strings.Join(args, "\n") + "\n"

	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "run with -update to create the golden file")
	assert.Equal(t, string(want), got)
}

// plainInput is a workload with no host access at all: the sandbox every
// other fixture is a difference from.
func plainInput() input {
	return input{
		Workload: types.EffectiveWorkload{
			Name: "chrome-work",
			Profile: &types.Profile{
				Name:     "work",
				Display:  21,
				Timezone: "Europe/London",
			},
			Workload: types.Workload{
				Name:    "chrome",
				Image:   "ghcr.io/qubesome/chrome:latest",
				Command: "/opt/google/chrome/chrome",
				Args:    []string{"--user-data-dir=/home/chrome/data"},
			},
		},
		Bundle: images.Bundle{
			Rootfs: "/store/unpacked/sha256-abc/rootfs",
			UID:    1000,
			GID:    1000,
			Env:    []string{"PATH=/usr/local/bin:/usr/bin:/bin"},
			Cwd:    "/home/chrome",
		},
		ProfileDir: "/run/user/1000/qubesome/work",
		UserDir:    "/run/user/1000/qubesome/work/user",
		ShmDir:     "/run/user/1000/qubesome/work/shm/chrome",
		CookiePath: "/run/user/1000/qubesome/work/.Xclient-cookie",
		SocketPath: "/run/user/1000/qubesome/work/qube.sock",
		Localtime:  []string{"/etc/localtime", "/usr/share/zoneinfo/Europe/London"},
	}
}

// grantedInput exercises every grant that changes the argument list at
// once: mime, gpu, usb, audio, camera, a named device and capsAdd.
func grantedInput() input {
	in := plainInput()

	in.Workload.Name = "kali-vpn-pentest"
	in.Workload.Profile.Name = "pentest"
	in.Workload.Workload.Name = "kali-vpn"
	in.Bundle.Cwd = "/home/kali"
	in.ProfileDir = "/run/user/1000/qubesome/pentest"
	in.UserDir = "/run/user/1000/qubesome/pentest/user"
	in.ShmDir = "/run/user/1000/qubesome/pentest/shm/kali-vpn"
	in.CookiePath = "/run/user/1000/qubesome/pentest/.Xclient-cookie"
	in.SocketPath = "/run/user/1000/qubesome/pentest/qube.sock"
	in.Workload.Workload.Command = "/usr/bin/openvpn"
	in.Workload.Workload.Args = []string{"--config", "/etc/vpn/client.ovpn"}
	in.Workload.Workload.HostAccess = types.HostAccess{
		Mime:       true,
		Gpus:       "all",
		Camera:     true,
		Microphone: true,
		Speakers:   true,
		USBDevices: []string{"1050:0407"},
		// The configuration carries the docker spelling, with no CAP_
		// prefix. bwrap rejects that form outright.
		CapsAdd: []string{"NET_ADMIN"},
		Devices: []string{"/dev/kvm"},
	}

	in.HomeDir = "/home/kali"
	in.QubesomeBin = "/usr/bin/qubesome"
	in.MTLSCA = "ca-pem"
	in.MTLSCert = "cert-pem"
	in.MTLSKey = "key-pem"
	in.VideoDevices = []string{"/dev/video0", "/dev/video1"}
	in.USBDevices = []string{"/dev/bus/usb/001/004", "/dev/hidraw9"}
	in.GPUNodes = []gpu.DeviceNode{
		{Path: "/dev/dri/renderD128"},
		{Path: "/dev/kfd"},
	}
	in.GPUMounts = []gpu.Mount{
		{
			HostPath:      "/usr/share/vulkan/icd.d/radeon_icd.x86_64.json",
			ContainerPath: "/usr/share/vulkan/icd.d/radeon_icd.x86_64.json",
		},
	}
	in.Paths = []sandbox.Mount{{Src: "/home/user/git", Dst: "/data/git"}}

	return in
}

func render(t *testing.T, in input) []string {
	t.Helper()

	spec, err := buildSpec(in)
	require.NoError(t, err)

	fd := seccompFD
	if !spec.Seccomp {
		fd = -1
	}

	args, err := sandbox.Args(spec, fd)
	require.NoError(t, err)

	return args
}

func TestSpecPlainWorkload(t *testing.T) {
	t.Parallel()

	golden(t, "plain", render(t, plainInput()))
}

func TestSpecGrantedWorkload(t *testing.T) {
	t.Parallel()

	golden(t, "granted", render(t, grantedInput()))
}

// The configuration carries capsAdd in the docker spelling, and bwrap
// rejects a bare NET_ADMIN as an unknown capability. It also applies
// capability arguments in order, so an add emitted before the --cap-drop
// ALL is undone by it silently, which for the VPN workload means a tunnel
// that never comes up and no message saying why.
func TestSpecCapabilityPrefixAndOrder(t *testing.T) {
	t.Parallel()

	args := render(t, grantedInput())

	drop := indexOfArg(args, "--cap-drop", "ALL")
	add := indexOfArg(args, "--cap-add", "CAP_NET_ADMIN")

	require.NotEqual(t, -1, drop)
	require.NotEqual(t, -1, add, "the CAP_ prefix is required, a bare NET_ADMIN is rejected")
	assert.Less(t, drop, add)

	assert.Equal(t, -1, indexOfArg(args, "--cap-add", "NET_ADMIN"))
}

func TestSpecLowercaseCapability(t *testing.T) {
	t.Parallel()

	in := plainInput()
	in.Workload.Workload.HostAccess.CapsAdd = []string{"net_admin", "CAP_SYS_PTRACE"}

	args := render(t, in)

	assert.NotEqual(t, -1, indexOfArg(args, "--cap-add", "CAP_NET_ADMIN"))
	assert.NotEqual(t, -1, indexOfArg(args, "--cap-add", "CAP_SYS_PTRACE"))
}

// The container runner needed --device for the cgroup rule and a bind on
// top for the host ACLs a security key relies on. --dev-bind is the bind,
// and it lifts the device restriction in the same step.
func TestSpecSharesAUSBDeviceOnce(t *testing.T) {
	t.Parallel()

	args := render(t, grantedInput())

	for _, node := range []string{"/dev/bus/usb/001/004", "/dev/hidraw9"} {
		assert.Equal(t, 1, countArg(args, "--dev-bind", node), node)
		assert.Equal(t, -1, indexOfArg(args, "--bind", node), node)
	}
}

// /dev/dri is shared as a directory and the render nodes sit inside it. A
// bind of the directory hides anything bound within it earlier.
func TestSpecSharesDriBeforeItsNodes(t *testing.T) {
	t.Parallel()

	args := render(t, grantedInput())

	assert.Less(t, indexOfArg(args, "--dev-bind", "/dev/dri"),
		indexOfArg(args, "--dev-bind", "/dev/dri/renderD128"))
}

// The video and audio groups are gone with the container runner. bwrap
// maps one uid and one gid, so a supplementary group does not survive, and
// the uaccess ACL that actually grants access names the invoking uid.
func TestSpecSharesCameraAndAudioNodesWithoutAGroup(t *testing.T) {
	t.Parallel()

	args := render(t, grantedInput())

	for _, node := range []string{"/dev/video0", "/dev/video1", "/dev/snd"} {
		assert.NotEqual(t, -1, indexOfArg(args, "--dev-bind", node), node)
	}
	assert.NotContains(t, args, "--group-add")
}

func TestSpecOmitsCameraAndAudioWhenNotGranted(t *testing.T) {
	t.Parallel()

	args := render(t, plainInput())

	assert.Equal(t, -1, indexOfArg(args, "--dev-bind", "/dev/snd"))
	assert.Equal(t, -1, indexOfArg(args, "--dev-bind", "/dev/video0"))
}

// A workload on the host dbus gets the whole host runtime directory, so
// the profile's isolated one is not mounted over it.
func TestSpecHostDbusSharesTheHostRuntimeDir(t *testing.T) {
	t.Parallel()

	in := plainInput()
	in.Workload.Workload.HostAccess.Dbus = true
	in.HostEnv = []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus"}

	args := render(t, in)

	assert.NotEqual(t, -1, indexOfArg(args, "--bind", "/run/user/1000"))
	assert.Equal(t, -1, indexOfArg(args, "--bind", in.UserDir))
	assert.NotEqual(t, -1, indexOfArg(args, "--ro-bind", "/etc/machine-id"))
	assert.NotEqual(t, -1, indexOfArg(args, "--setenv", "DBUS_SESSION_BUS_ADDRESS"))
}

// Without the host dbus the workload gets the profile's runtime directory
// and the machine-id generated for the profile.
func TestSpecIsolatedRunUser(t *testing.T) {
	t.Parallel()

	in := plainInput()
	args := render(t, in)

	i := indexOfArg(args, "--bind", in.UserDir)
	require.NotEqual(t, -1, i)
	assert.Equal(t, "/run/user/1000", args[i+2])

	assert.NotEqual(t, -1,
		indexOfArg(args, "--ro-bind", filepath.Join(in.ProfileDir, "machine-id")))
}

// /etc/localtime is usually a symlink, and the link alone resolves to
// nothing inside the sandbox.
func TestSpecSharesLocaltimeAndItsTarget(t *testing.T) {
	t.Parallel()

	args := render(t, plainInput())

	assert.NotEqual(t, -1, indexOfArg(args, "--ro-bind", "/etc/localtime"))
	assert.NotEqual(t, -1, indexOfArg(args, "--ro-bind", "/usr/share/zoneinfo/Europe/London"))
}

// There is no uplink in this stage, so a workload with anything short of
// the host network reaches itself and nothing else. A named network is
// one of those: the name only means something once the gateway lands.
func TestSpecUnsharesTheNetworkWithoutAHostGrant(t *testing.T) {
	t.Parallel()

	for _, network := range []string{"", "none", "qubesome"} {
		in := plainInput()
		in.Workload.Workload.HostAccess.Network = network

		spec, err := buildSpec(in)
		require.NoError(t, err)
		assert.Equal(t, sandbox.NetNone, spec.Net, network)

		assert.Contains(t, render(t, in), "--unshare-net", network)
	}
}

// A workload granted the host network gets it. The grant used to reach a
// spec that unshared the network regardless, which is a silent downgrade
// of the one network grant a sandbox can honour, so the golden file below
// is what stands between it and a repeat.
func TestSpecHostNetworkIsNotUnshared(t *testing.T) {
	t.Parallel()

	in := plainInput()
	in.Workload.Workload.HostAccess.Network = "host"

	spec, err := buildSpec(in)
	require.NoError(t, err)
	assert.Equal(t, sandbox.NetHost, spec.Net)

	assert.NotContains(t, render(t, in), "--unshare-net")
}

func TestSpecHostNetworkWorkload(t *testing.T) {
	t.Parallel()

	in := plainInput()
	in.Workload.Workload.HostAccess.Network = "host"

	golden(t, "hostnet", render(t, in))
}

// Chromium builds a user namespace for its own sandbox, so a workload
// cannot be blocked from nesting one the way a profile is.
func TestSpecKeepsNestedUserNamespaces(t *testing.T) {
	t.Parallel()

	assert.NotContains(t, render(t, plainInput()), "--disable-userns")
}

func TestSpecSeccompUnconfined(t *testing.T) {
	t.Parallel()

	in := plainInput()
	in.Workload.Workload.HostAccess.SeccompUnconfined = true

	spec, err := buildSpec(in)
	require.NoError(t, err)
	assert.False(t, spec.Seccomp)

	assert.NotContains(t, render(t, in), "--seccomp")
}

// Only one uid and one gid are mapped into the sandbox, so a workload
// asking to run as another user moves both.
func TestSpecUserOverridesTheBundle(t *testing.T) {
	t.Parallel()

	user := 0
	in := plainInput()
	in.Workload.Workload.User = &user

	spec, err := buildSpec(in)
	require.NoError(t, err)

	assert.Equal(t, 0, spec.UID)
	assert.Equal(t, 0, spec.GID)
}

// The name the container runner gave the container is now the hostname and
// the key of the sandbox state file.
func TestSpecHostnameIsTheWorkloadName(t *testing.T) {
	t.Parallel()

	spec, err := buildSpec(plainInput())
	require.NoError(t, err)

	assert.Equal(t, "chrome-work", spec.Hostname)
}

// bwrap has no image entrypoint to fall back on, and the unpacked bundle
// does not carry one either.
func TestSpecRequiresACommand(t *testing.T) {
	t.Parallel()

	in := plainInput()
	in.Workload.Workload.Command = ""

	_, err := buildSpec(in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no command")
}

// A bind mount puts the host node where it is asked for, with the access
// the host node already grants. It cannot recreate a node under another
// name, and it cannot narrow the permissions, so both are refused rather
// than quietly granted in full.
func TestSpecRejectsDeviceRequestsABindCannotHonour(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		device string
		want   string
	}{
		"remapped":   {device: "/dev/kvm:/dev/other", want: "remap"},
		"restricted": {device: "/dev/kvm:/dev/kvm:r", want: "permissions"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			in := plainInput()
			in.Workload.Workload.HostAccess.Devices = []string{tc.device}

			_, err := buildSpec(in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// The mTLS values reach the sandbox through --setenv, which PackArgs moves
// into a memfd. A command line is world readable through /proc, and the
// container runners kept these in the runner's environment instead.
func TestMTLSStaysOffTheCommandLine(t *testing.T) {
	t.Parallel()

	in := grantedInput()

	spec, err := buildSpec(in)
	require.NoError(t, err)
	assert.Contains(t, spec.Env, "Q_MTLS_KEY=key-pem")

	args, err := sandbox.Args(spec, seccompFD)
	require.NoError(t, err)

	outer, packed, err := sandbox.PackArgs(spec, args, seccompFD+1)
	require.NoError(t, err)
	defer packed.Close()

	for _, a := range outer {
		assert.NotContains(t, a, "key-pem")
		assert.NotContains(t, a, "cert-pem")
	}

	// The separator and the command stay on the command line. bwrap reads
	// a "--" inside the descriptor as the end of the descriptor and drops
	// everything after it, so a command packed with the options would
	// never run.
	assert.Equal(t, []string{"--args", "4", "--", "/usr/bin/openvpn", "--config", "/etc/vpn/client.ovpn"}, outer)
}

func TestSpecOmitsMTLSWithoutMime(t *testing.T) {
	t.Parallel()

	spec, err := buildSpec(plainInput())
	require.NoError(t, err)

	for _, e := range spec.Env {
		assert.False(t, strings.HasPrefix(e, "Q_MTLS_"), e)
	}
}

func TestHostEnvSkipsUnsetVariables(t *testing.T) {
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	t.Setenv("XDG_SESSION_ID", "")
	require.NoError(t, os.Unsetenv("XDG_RUNTIME_DIR"))

	env := hostEnv()

	assert.Contains(t, env, "DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus")
	assert.Contains(t, env, "XDG_SESSION_ID=")
	for _, e := range env {
		assert.False(t, strings.HasPrefix(e, "XDG_RUNTIME_DIR="), e)
	}
}

// supervisedInput is a plain workload that is single instance, which 26 of
// the 28 workloads in the reference configuration are. The sandbox runs the
// supervisor and the supervisor runs the workload.
func supervisedInput() input {
	in := plainInput()

	in.Workload.Workload.SingleInstance = true
	in.QubesomeBin = "/usr/bin/qubesome"
	in.AgentDir = "/run/user/1000/qubesome/work/agent/chrome"

	return in
}

func TestSpecSupervisedWorkload(t *testing.T) {
	t.Parallel()

	golden(t, "supervised", render(t, supervisedInput()))
}

// A sandbox cannot be entered, so a second launch of a single instance
// workload is handed to the supervisor already inside it. That only works
// if the supervisor is what the sandbox runs.
func TestSpecSingleInstanceRunsTheSupervisor(t *testing.T) {
	t.Parallel()

	in := supervisedInput()
	args := render(t, in)

	i := slices.Index(args, "--")
	require.NotEqual(t, -1, i)

	assert.Equal(t, []string{
		files.InProfileBinary,
		sandbox.SuperviseCommand,
		"/opt/google/chrome/chrome",
		"--user-data-dir=/home/chrome/data",
	}, args[i+1:])

	assert.NotEqual(t, -1, indexOfArg(args, "--ro-bind", in.QubesomeBin))
	assert.NotEqual(t, -1, indexOfArg(args, "--bind", in.AgentDir))
}

func TestSpecWorkloadThatIsNotSingleInstanceRunsItsCommand(t *testing.T) {
	t.Parallel()

	args := render(t, plainInput())

	i := slices.Index(args, "--")
	require.NotEqual(t, -1, i)

	assert.Equal(t, []string{
		"/opt/google/chrome/chrome",
		"--user-data-dir=/home/chrome/data",
	}, args[i+1:])

	assert.NotContains(t, args, files.InWorkloadAgentDir())
}

// The socket is created by the supervisor inside the sandbox, so what is
// shared is the directory holding it, and it has to be writable.
func TestSpecSharesTheAgentDirRatherThanTheSocket(t *testing.T) {
	t.Parallel()

	in := supervisedInput()
	args := render(t, in)

	assert.Equal(t, -1, indexOfArg(args, "--ro-bind", in.AgentDir))
	assert.Equal(t, 1, countArg(args, "--bind", in.AgentDir))
}

// The mime handler and the supervisor are the same binary.
func TestSpecSharesTheBinaryOnceForMimeAndTheSupervisor(t *testing.T) {
	t.Parallel()

	in := grantedInput()
	in.Workload.Workload.SingleInstance = true
	in.AgentDir = "/run/user/1000/qubesome/pentest/agent/kali-vpn"

	args := render(t, in)

	assert.Equal(t, 1, countArg(args, "--ro-bind", in.QubesomeBin))
}

// A single instance workload whose sandbox does not run a supervisor
// answers nothing, and every later launch of it starts another sandbox
// against the same data.
func TestSpecRejectsSingleInstanceWithoutASupervisor(t *testing.T) {
	t.Parallel()

	in := supervisedInput()
	in.AgentDir = ""

	_, err := buildSpec(in)
	require.Error(t, err)

	in = supervisedInput()
	in.QubesomeBin = ""

	_, err = buildSpec(in)
	require.Error(t, err)
}

func TestStatePathRejectsANameThatIsNotOneComponent(t *testing.T) {
	t.Parallel()

	ew := types.EffectiveWorkload{
		Name:    "../escape",
		Profile: &types.Profile{Name: "work"},
	}

	_, err := StatePath(ew)
	require.Error(t, err)
}

// supervised sets HOME so the profile's run directory is a temporary one,
// and returns a workload whose command is argv. It cannot run in parallel
// for the same reason.
func supervised(t *testing.T, argv []string) types.EffectiveWorkload {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	return types.EffectiveWorkload{
		Name:    "chrome-work",
		Profile: &types.Profile{Name: "work"},
		Workload: types.Workload{
			Name:           "chrome",
			SingleInstance: true,
			Command:        argv[0],
			Args:           argv[1:],
		},
	}
}

// serveWorkload starts a supervisor where the host will look for one, and
// returns once it is answering.
func serveWorkload(t *testing.T, ew types.EffectiveWorkload) {
	t.Helper()

	dir, err := files.WorkloadAgentDir(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, files.DirMode))

	socket, err := files.WorkloadAgentSocket(ew.Profile.Name, ew.Workload.Name)
	require.NoError(t, err)

	// The main command holds the sandbox open and lets go of the standard
	// streams, which are the test binary's own.
	go func() {
		_ = sandbox.Supervise(socket, []string{"/bin/sh", "-c", "exec >/dev/null 2>&1; sleep 5"})
	}()

	require.Eventually(t, func() bool {
		return sandbox.Spawn(socket, []string{"/bin/sh", "-c", "exit 0"}) == nil
	}, 5*time.Second, 10*time.Millisecond)
}

func liveState(t *testing.T, ew types.EffectiveWorkload) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(files.ProfileDir(ew.Profile.Name), files.DirMode))

	statePath, err := StatePath(ew)
	require.NoError(t, err)

	// This process is the live one. What the state file has to name is a
	// pid that is running, and nothing here reads any further into it.
	require.NoError(t, sandbox.WriteState(statePath, os.Getpid()))

	return statePath
}

func TestHandOverToARunningSandbox(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	ew := supervised(t, []string{"/bin/sh", "-c", "printf handed > " + out})

	statePath := liveState(t, ew)
	serveWorkload(t, ew)

	handled, err := handOver(ew, statePath)
	require.NoError(t, err)
	require.True(t, handled)

	require.Eventually(t, func() bool {
		data, err := os.ReadFile(out)
		return err == nil && string(data) == "handed"
	}, 5*time.Second, 10*time.Millisecond)
}

// The interesting case. The sandbox is recorded as running and nothing in
// it answers, so the launch falls through to a fresh sandbox rather than
// leaving the user with an application that does not open.
func TestHandOverWithALiveStateFileAndADeadSocket(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh"})
	statePath := liveState(t, ew)

	handled, err := handOver(ew, statePath)
	require.NoError(t, err)
	assert.False(t, handled)
}

func TestHandOverWithNoStateFile(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh"})

	statePath, err := StatePath(ew)
	require.NoError(t, err)

	handled, err := handOver(ew, statePath)
	require.NoError(t, err)
	assert.False(t, handled)
}

// A state file naming a pid that is gone is a cache entry, not an error,
// and it must not cost the launch the startup grace either.
func TestHandOverWithADeadStateFile(t *testing.T) {
	ew := supervised(t, []string{"/bin/sh"})
	statePath := liveState(t, ew)

	require.NoError(t, os.WriteFile(statePath, []byte(`{"pid":2147483646,"startTime":1}`), files.FileMode))

	start := time.Now()
	handled, err := handOver(ew, statePath)
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Less(t, time.Since(start), startupGrace)
}

// A supervisor that answers and refuses is running the workload, so the
// launch reports the refusal. A second sandbox would put two of a single
// instance workload on one profile directory.
func TestHandOverReportsARefusal(t *testing.T) {
	ew := supervised(t, []string{filepath.Join(t.TempDir(), "not-there")})

	statePath := liveState(t, ew)
	serveWorkload(t, ew)

	handled, err := handOver(ew, statePath)
	require.Error(t, err)
	assert.NotErrorIs(t, err, sandbox.ErrNoSupervisor)
	assert.True(t, handled)
}

func indexOfArg(args []string, flag, value string) int {
	for i := range args {
		if args[i] == flag && i+1 < len(args) && args[i+1] == value {
			return i
		}
	}

	return -1
}

func countArg(args []string, flag, value string) int {
	n := 0
	for i := range args {
		if args[i] == flag && i+1 < len(args) && args[i+1] == value {
			n++
		}
	}

	return n
}

// A mapping is src:dst with an optional :ro flag, and cutting at the
// first colon left the flag on the destination. Every read-only mapping
// in a real configuration landed at a path with ":ro" on the end, so a
// dotfile was present under a name nothing reads, and it was writable.
func TestMappedPathsSplitsTheReadOnlyFlagOff(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	require.NoError(t, os.WriteFile(rc, nil, 0o600))

	got := mappedPaths([]string{
		rc + ":/home/coder/.zshrc:ro",
		dir + ":/home/coder/git",
	})

	require.Equal(t, []sandbox.Mount{
		{Src: rc, Dst: "/home/coder/.zshrc", ReadOnly: true},
		{Src: dir, Dst: "/home/coder/git"},
	}, got)
}
