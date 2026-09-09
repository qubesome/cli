package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/seccomp"
	"github.com/qubesome/cli/internal/session"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/mtls"
	"github.com/qubesome/cli/pkg/control"
	"golang.org/x/sys/execabs"
	"golang.org/x/sys/unix"
)

// These are the gateway image's own paths rather than qubesome's layout,
// which is why they are here and not in internal/files.
const (
	// inConfigPath is where the policy file is bound inside the sandbox. It
	// is the image's own default for CONFIG_PATH, so a policy taken from the
	// config tree lands where the gateway already looks for one.
	inConfigPath = "/etc/qubesome/gateway.yml"

	// inSecretsDir is the directory the gateway resolves a policy's
	// valueFrom.file references against, and the root it refuses to read
	// outside of. It is the image's default too, so a policy names paths
	// under /run/secrets whatever the host directory is called.
	inSecretsDir = "/run/secrets"

	// inSocketDir is where the session's socket directory is bound.
	inSocketDir = "/run/qubesome-gateway"

	// gatewayCommand is the gateway image's entrypoint. An unpacked bundle
	// does not carry one and bwrap has no fallback to it, so the command has
	// to be named here.
	gatewayCommand = "/usr/bin/gateway"

	// gatewayHostname is what the sandbox calls itself. There is one gateway
	// for the whole session, so unlike a workload's it carries no profile.
	gatewayHostname = "qubesome-gateway"

	// pastaCommand is the uplink binary in the gateway image, where the
	// passt package puts it.
	pastaCommand = "/usr/bin/pasta"

	// tunDevice is the node pasta opens to create the tap it puts in the
	// gateway's network namespace. bwrap's --dev makes a small set of nodes
	// and this is not one of them, so it is bound in by name.
	tunDevice = "/dev/net/tun"
)

// The descriptors the gateway sandbox is handed, in the order they are put
// in exec.Cmd.ExtraFiles, which os/exec numbers from 3 upwards. The seccomp
// filter, the packed arguments and the info pipe are read and written by the
// inner bwrap and the namespace by the outer one, and all four pass through
// the outer bwrap unchanged.
const (
	seccompFD = 3
	packedFD  = 4
	usernsFD  = 5
	infoFD    = 6
)

// helperUsernsFD is the session namespace's descriptor in a helper. A helper
// is handed nothing else, so it is the first one os/exec numbers.
const helperUsernsFD = 3

// Gateway names the files one session's gateway is kept in.
//
// The paths are carried rather than read from files at each use, for the
// reason session.Session carries its own: a test can then drive a gateway
// outside the user's run directory.
type Gateway struct {
	Dir        string
	LockPath   string
	StatePath  string
	AllocPath  string
	CredsPath  string
	Socket     string
	SocketDir  string
	SecretsDir string
}

// Current returns the gateway of the session of the user running qubesome.
func Current() Gateway {
	return Gateway{
		Dir:        files.SessionDir(),
		LockPath:   files.GatewayLockPath(),
		StatePath:  files.GatewayStatePath(),
		AllocPath:  files.GatewayAllocPath(),
		CredsPath:  files.GatewayCredsPath(),
		Socket:     files.GatewaySocket(),
		SocketDir:  files.GatewaySocketDir(),
		SecretsDir: files.GatewaySecretsDir(),
	}
}

// Up makes sure the session's gateway is running and returns once it is
// ready to police traffic.
//
// cfg is the gateway block of the qubesome config and root is the directory
// that config was read from, which is what the policy file path is resolved
// against.
func (g Gateway) Up(cfg types.GatewayConfig, root string) error {
	if err := os.MkdirAll(g.Dir, files.DirMode); err != nil {
		return fmt.Errorf("failed to create the session dir %q: %w", g.Dir, err)
	}

	if err := g.startOnce(cfg, root); err != nil {
		return err
	}

	return g.ready()
}

// Client returns a client for this session's gateway, presenting the
// credentials the launch that started it left behind.
func (g Gateway) Client() (*Client, error) {
	c, err := g.readCreds()
	if err != nil {
		return nil, err
	}

	return NewClientWithCreds(g.Socket, c.CA, c.Cert, c.Key), nil
}

// SandboxPID returns this session's gateway sandbox, in the host's pid
// namespace.
//
// It is the pid a veth's gateway end is put next to and the one pasta is
// pointed at, so it is read from the record the launch wrote rather than
// worked out again. A state file outlives the process it names, which is why
// this reports no gateway rather than a pid whenever the record has been
// left behind by one that crashed.
func (g Gateway) SandboxPID() (int, error) {
	if !sandbox.Alive(g.StatePath) {
		return 0, ErrNoGateway
	}

	st, err := sandbox.ReadState(g.StatePath)
	if err != nil {
		return 0, err
	}

	return st.PID, nil
}

// startOnce starts the gateway unless one is already running.
//
// One per session, on demand, and not one per profile: the policy file is
// keyed by workload across every profile, so a gateway per profile would
// have to be handed a slice of a file that is not written in slices.
//
// The singleton is the lock and the state file together, and not a lock held
// for the gateway's lifetime as the session holder's is. The gateway runs an
// image qubesome did not write and cannot ask to hold a lock, so what the
// lock covers is the check and the start, which every launch takes in turn.
// A state file left behind by a crash reads as not running, which is the
// property that file exists for.
//
// The lock is released before the readiness wait rather than held across it.
// A launch arriving while the gateway is still coming up has to wait for the
// same event, and the Ready call is where that waiting belongs. Holding the
// lock would make it wait twice for one thing.
func (g Gateway) startOnce(cfg types.GatewayConfig, root string) error {
	lock, err := acquire(g.LockPath)
	if err != nil {
		return err
	}
	defer lock.Close()

	if sandbox.Alive(g.StatePath) {
		slog.Debug("[gateway] the session gateway is already running")
		return nil
	}

	return g.start(cfg, root)
}

// acquire takes the gateway lock and returns the file that holds it.
//
// LOCK_EX and not LOCK_EX|LOCK_NB, which is the opposite of the session
// lock. Two launches racing to start the gateway both have to end up using
// it, so the loser waits and then finds the winner's gateway running, where
// a refusal would stop a workload that has nothing wrong with it. The wait
// is an image pull at worst, which is what the launch was going to cost
// anyway.
func acquire(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, files.FileMode)
	if err != nil {
		return nil, fmt.Errorf("failed to open the gateway lock %q: %w", path, err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to lock %q: %w", path, err)
	}

	return f, nil
}

// start builds the gateway's sandbox and launches it.
//
// Everything that can be answered without touching the network is answered
// first, so a configuration mistake is reported before an image is pulled
// for it.
func (g Gateway) start(cfg types.GatewayConfig, root string) error {
	configPath, err := cfg.ConfigPath(root)
	if err != nil {
		return err
	}
	fi, err := os.Stat(configPath)
	if err != nil {
		return fmt.Errorf("the gateway config %q cannot be read: %w", configPath, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("the gateway config %q is a directory", configPath)
	}

	if err := g.prepareDirs(); err != nil {
		return err
	}

	// A gateway that was killed leaves its socket behind and a bind to a
	// path that already exists fails. Nothing else creates this one, and the
	// lock and the state check above say no gateway is listening on it.
	if err := os.Remove(g.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove the stale gateway socket %q: %w", g.Socket, err)
	}

	creds, err := mtls.NewCredentialsFor(control.ServerName)
	if err != nil {
		return fmt.Errorf("failed to mint the gateway control credentials: %w", err)
	}
	if err := g.writeCreds(creds); err != nil {
		return err
	}

	bundle, err := images.PullProfileImage(cfg.Image)
	if err != nil {
		return fmt.Errorf("failed to get the gateway image %q: %w", cfg.Image, err)
	}

	return g.launch(bundle, g.spec(bundle, configPath, creds))
}

// prepareDirs creates the host directories the sandbox is given.
func (g Gateway) prepareDirs() error {
	// The gateway creates the control socket in this one, so it is bound
	// writable and has to exist first.
	if err := os.MkdirAll(g.SocketDir, files.DirMode); err != nil {
		return fmt.Errorf("failed to create the gateway socket dir %q: %w", g.SocketDir, err)
	}

	// Read-only inside, and empty until the user puts something in it. It is
	// created rather than required so that a policy with no secret
	// references does not need a directory the user has never heard of.
	if err := os.MkdirAll(g.SecretsDir, files.DirMode); err != nil {
		return fmt.Errorf("failed to create the gateway secrets dir %q: %w", g.SecretsDir, err)
	}

	return nil
}

// launch starts the gateway sandbox, gives it its uplink, and records it.
//
// It is recorded last. Everything before that point can fail, and a gateway
// found alive by the next launch is one that launch will use, so a gateway
// that has no pid to wire workloads to or no uplink to reach anything
// through must not be left behind looking healthy.
func (g Gateway) launch(bundle images.Bundle, spec sandbox.Spec) error {
	filter, err := seccomp.MemFD()
	if err != nil {
		return err
	}
	defer filter.Close()

	args, err := sandbox.Args(spec, seccompFD)
	if err != nil {
		return err
	}

	// The server half of the control credentials is in the environment, and
	// a command line is world readable through /proc. Only the descriptor
	// holding the options, and the command, stay on it.
	outer, packed, err := sandbox.PackArgs(spec, sandbox.InfoFDArgs(args, infoFD), packedFD)
	if err != nil {
		return err
	}
	defer packed.Close()

	sess := session.Current()
	if err := sess.Start(); err != nil {
		return err
	}

	ns, err := sess.Open(usernsFD)
	if err != nil {
		return err
	}
	defer ns.Close()

	info, report, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create the gateway info pipe: %w", err)
	}
	defer info.Close()

	cmd := execabs.Command(files.BwrapBinary, ns.Enter(outer)...) //nolint:gosec // the arguments are built from the gateway config.
	cmd.ExtraFiles = []*os.File{filter, packed, ns.File(), report}

	// The gateway outlives the launch that started it, and a qubesome run
	// typed at a terminal sits in the shell's foreground process group. A
	// session whose gateway ended at the first Ctrl-C would take the egress
	// of every workload still running with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()

	// The sandbox has its own copy of the write end now, and this one is of
	// no further use. It is closed whether or not the start worked, since
	// otherwise this process would be holding a pipe nothing will ever
	// write to.
	report.Close()

	if err != nil {
		return fmt.Errorf("failed to start the gateway sandbox: %w", err)
	}

	gw := running{cmd: cmd}

	gw.pid, err = sandbox.ChildPID(info, sandbox.InfoGrace)
	if err != nil {
		return gw.stop(err)
	}

	gw.uplink, err = uplink(bundle.Rootfs, gw.pid)
	if err != nil {
		return gw.stop(err)
	}

	// An uplink that is gone before it was ever useful is a gateway with no
	// egress, and starting one is worse than not starting: every workload
	// after it would come up policed but unable to reach anything, with the
	// only sign a warning in the log of whoever started the session.
	//
	// cmd.Start succeeding says only that bwrap ran. It says nothing about
	// pasta, which is what bwrap then executes, so a gateway image missing
	// it fails here rather than at Start. That is exactly what a missing
	// /usr/bin/pasta produced: bwrap reported execvp failed, the launch
	// carried on, and the uplink was reported lost a moment later as though
	// it had once been there.
	if err := stillUp(gw.uplink, uplinkGrace); err != nil {
		return gw.stop(err)
	}

	// The sandbox's own init is recorded and not the bwrap that started it.
	// It is the pid every namespace path is built from, so a later launch
	// with a workload to wire has to be able to read it back. It is also the
	// truer answer to whether the gateway is running: killing the outer
	// bwrap does not signal the sandbox below it, and a sandbox that has
	// gone is a gateway that has gone whatever is left above it.
	if err := sandbox.WriteState(g.StatePath, gw.pid); err != nil {
		return gw.stop(fmt.Errorf("failed to record the gateway state: %w", err))
	}

	// This gateway holds an empty map, so the addresses its predecessor was
	// told about mean nothing to it and the count starts again.
	if err := os.Remove(g.AllocPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to reset the gateway addresses", "path", g.AllocPath, "error", err)
	}

	slog.Debug("[gateway] started the session gateway", "pid", cmd.Process.Pid, "sandbox", gw.pid)

	go reap(cmd, g.StatePath)

	return nil
}

// running is a gateway sandbox that has started but is not yet usable.
type running struct {
	// cmd is the bwrap that was started, which is the outer one of the two.
	cmd *execabs.Cmd

	// pid is the sandbox's own init process in the host's pid namespace,
	// which is what bwrap reports on its info descriptor. It is the pid
	// every namespace path is built from, so it is carried rather than
	// worked out again from the process tree.
	pid int

	// uplink is pasta, or nil before it has been started.
	uplink *execabs.Cmd
}

// stop takes down a gateway that cannot be used and returns why.
//
// A gateway nothing can find again is worse than none, and so is one with no
// uplink: the next launch would find it alive, skip starting one and hand a
// workload an address on a gateway that reaches nothing. Killing it here
// means the next launch starts one properly.
func (r running) stop(cause error) error {
	kill(r.uplink)

	// The bwrap that was started is the outer one, and killing it does not
	// signal the sandbox nested below it. The sandbox's own init is pid 1 of
	// its pid namespace, and a SIGKILL from an ancestor namespace takes the
	// whole namespace with it, so that is the one to name.
	if r.pid > 0 {
		if err := syscall.Kill(r.pid, syscall.SIGKILL); err != nil {
			slog.Warn("failed to kill the unusable gateway sandbox", "pid", r.pid, "error", err)
		}
	}

	kill(r.cmd)

	return cause
}

// kill ends a started process and reaps it. A process that was never started
// is not an error to pass here, which is what lets stop take a gateway down
// at any point in its launch.
func kill(cmd *execabs.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	if err := cmd.Process.Kill(); err != nil {
		slog.Warn("failed to kill a gateway process", "pid", cmd.Process.Pid, "error", err)
	}

	_ = cmd.Wait()
}

// uplink gives the gateway its egress and returns the process that is it.
//
// pasta does not run inside the gateway's network namespace, and the natural
// reading is the one that cannot work. It holds its sockets in the host's
// namespace and creates a tap device in the target one, so a pasta confined
// to the namespace it serves would have nothing to serve it from. It runs
// here as a second short-lived sandbox from the gateway image's own rootfs,
// in the session user namespace, with no --unshare-net, and it is pointed at
// the gateway's namespace by path.
//
// Nothing on the host is privileged for it. CAP_NET_ADMIN creates the tap
// and CAP_SYS_ADMIN enters the namespace, and both are held in the session
// namespace. A capability reaches every descendant of the namespace it is
// held in, so from there it reaches the gateway's namespace, which is nested
// below, and nothing above or beside it.
func uplink(rootfs string, pid int) (*execabs.Cmd, error) {
	cmd, err := helper{
		Rootfs:  rootfs,
		Caps:    []string{"CAP_NET_ADMIN", "CAP_SYS_ADMIN"},
		Devices: []string{tunDevice},
		Args:    pastaArgs(pid),
	}.start()
	if err != nil {
		return nil, fmt.Errorf("failed to start the gateway uplink: %w", err)
	}

	slog.Debug("[gateway] started the gateway uplink", "pid", cmd.Process.Pid, "netns", sandbox.NetnsPath(pid))

	return cmd, nil
}

// pastaArgs is the uplink's own command line, inside the helper sandbox.
func pastaArgs(pid int) []string {
	return []string{
		pastaCommand,

		// It stays in the foreground so that its going away is something
		// this process can see. pasta puts itself in the background
		// otherwise, and the gateway would lose its egress with nothing
		// anywhere saying so.
		"--foreground",

		// pasta addresses the tap end inside the target namespace itself.
		// The gateway image runs no DHCP client, so without this the tap
		// comes up with no address and the gateway reaches nothing.
		"--config-net",

		// No port is forwarded in either direction. Inbound forwarding
		// would publish the gateway's own proxy listeners on the host,
		// where anything running there could reach them directly, without
		// the address that tells the gateway which workload it is talking
		// to and therefore which policy to apply.
		"-t", "none",
		"-u", "none",
		"-T", "none",
		"-U", "none",

		"--netns", sandbox.NetnsPath(pid),
	}
}

// uplinkGrace is how long the uplink has to fail before its going away is
// read as a failure to start rather than as a loss of egress later. It is
// short because the failures it catches are immediate: a binary that is not
// in the image, or one that refuses its arguments.
const uplinkGrace = 500 * time.Millisecond

// stillUp reports whether the uplink survived its first moments.
//
// It waits rather than polling the process table, because a command that
// exits is only reapable once, and watchUplink is what reaps it afterwards.
func stillUp(cmd *execabs.Cmd, grace time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// It is already reaped, so nothing else may wait on it. Replacing
		// the process leaves watchUplink with a command it cannot wait on
		// twice, which is why this path returns rather than starting it.
		return fmt.Errorf("the gateway uplink did not start: %w", err)
	case <-time.After(grace):
		// Hand the wait back. The goroutine above still owns it, so
		// watchUplink is given the channel rather than the command.
		// Losing it later is reported and not repaired. Restarting it
		// would be a session that cannot say whether what it is doing is
		// policed, and a workload that briefly could not resolve a name
		// is the better of those two.
		go func() {
			slog.Warn("the session gateway has lost its uplink and has no egress; "+
				"restart the session to give it one", "error", <-done)
		}()

		return nil
	}
}

// helper is a short-lived command run from the gateway image's rootfs inside
// the session's user namespace.
//
// The tools it runs come from the image rather than the host, so a host with
// no iproute2 and no passt on it can still run a session, and the versions
// qubesome drives are the ones the image was tested with.
type helper struct {
	// Rootfs is the gateway image's root filesystem.
	Rootfs string

	// Caps are the capabilities it keeps, named the way bwrap names them.
	// They are held in the session's user namespace, so they reach every
	// sandbox started under it and nothing on the host.
	Caps []string

	// Devices are host device nodes it needs, which bwrap's own --dev does
	// not make.
	Devices []string

	// OwnNet gives the helper a network namespace of its own.
	//
	// It decides whether the helper may create a link at all. A netlink
	// request is authorised against the namespace the caller is standing
	// in, so a helper in the host's namespace needs CAP_NET_ADMIN over
	// the host's, which an ordinary user has nowhere. In one it made
	// itself, the owner is the session's user namespace and the same
	// capability applies.
	//
	// That is what creating a veth failed on, with RTNETLINK answers:
	// Operation not permitted, while both namespaces the ends were bound
	// for were reachable. Moving an end into a target namespace is a
	// separate permission, held over the target's owner, and the session
	// namespace covers every sandbox nested under it.
	//
	// pasta is the helper that must not have one. It holds its sockets in
	// the host's namespace and puts a tap in the target, so a namespace
	// of its own would leave it serving from nowhere.
	OwnNet bool

	// Args is the command, argv[0] first.
	Args []string

	// Stdin is what the command reads, or nil for nothing. It is how a
	// helper is given work to do without any of it appearing on a command
	// line or passing through a shell.
	Stdin io.Reader
}

// command builds the helper's process without starting it, and returns the
// session namespace handle the caller has to keep open until it has.
func (h helper) command() (*execabs.Cmd, *session.Namespace, error) {
	args, err := h.bwrapArgs(helperUsernsFD)
	if err != nil {
		return nil, nil, err
	}

	ns, err := session.Current().Open(helperUsernsFD)
	if err != nil {
		return nil, nil, err
	}

	cmd := execabs.Command(files.BwrapBinary, args...) //nolint:gosec // the arguments are built from the gateway config and this process's own pids.
	cmd.ExtraFiles = []*os.File{ns.File()}
	cmd.Stdin = h.Stdin

	return cmd, ns, nil
}

// start runs the helper and returns the process without waiting for it.
func (h helper) start() (*execabs.Cmd, error) {
	cmd, ns, err := h.command()
	if err != nil {
		return nil, err
	}
	defer ns.Close()

	// A helper started this way outlives the launch that started it, for the
	// same reason the gateway does. pasta is the session's egress and a
	// Ctrl-C at the terminal that started a workload is not a request to
	// take it away.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

// run runs the helper to completion and reports what it said if it failed.
//
// The output is collected rather than passed through, because a helper that
// works says nothing and a helper that does not is the only explanation
// there will be of why a launch stopped.
func (h helper) run() error {
	cmd, ns, err := h.command()
	if err != nil {
		return err
	}
	defer ns.Close()

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		said := strings.TrimSpace(out.String())
		if said == "" {
			return fmt.Errorf("%s: %w", h.Args[0], err)
		}

		return fmt.Errorf("%s: %w: %s", h.Args[0], err, said)
	}

	return nil
}

// bwrapArgs renders the helper into bwrap arguments.
//
// usernsFD is the descriptor the session's user namespace has in the child,
// which os/exec numbers from 3 upwards in the order of exec.Cmd.ExtraFiles.
//
// This joins the session's user namespace rather than nesting a new one
// below it, which is the whole point of a helper and the reason it cannot go
// through sandbox.Args. A capability is held in the namespace it is granted
// in and reaches that namespace's descendants, so a helper in a namespace
// nested below the session's would hold nothing over the gateway's, which is
// beside it rather than below. bwrap also refuses --userns together with
// --unshare-user, which sandbox.Args always passes.
//
// The uid is left alone for a related reason. A joined namespace carries the
// holder's single uid mapping and nothing else, so --uid would fail rather
// than change anything, and the capabilities are what the helper needs
// rather than a particular uid.
//
// There is no seccomp filter here, and that is not an oversight. The
// vendored profile denies setns outright, because it is gated on
// CAP_SYS_ADMIN and a sandbox that drops every capability can never hold it.
// Entering a namespace is the one thing a helper exists to do.
func (h helper) bwrapArgs(usernsFD int) ([]string, error) {
	if h.Rootfs == "" {
		return nil, errors.New("gateway: the helper has no rootfs")
	}
	if len(h.Args) == 0 {
		return nil, errors.New("gateway: the helper has no command")
	}
	if usernsFD < 3 {
		return nil, fmt.Errorf("gateway: namespace descriptor %d collides with the standard streams", usernsFD)
	}

	args := make([]string, 0, 22+2*len(h.Caps)+3*len(h.Devices)+len(h.Args))
	args = append(args,
		"--userns", strconv.Itoa(usernsFD),

		// The image is shared read-only and the writes a helper makes are
		// discarded with it, as a sandbox's are.
		"--overlay-src", h.Rootfs,
		"--tmp-overlay", "/",

		// The pid namespace is never unshared. It is what makes
		// /proc/<pid>/ns/net name the sandbox a helper is pointed at,
		// and a fresh one would carry a /proc listing only the helper.
		"--unshare-ipc",
		"--unshare-uts",
		"--unshare-cgroup",

		"--cap-drop", "ALL",
	)

	if h.OwnNet {
		args = append(args, "--unshare-net")
	}

	// bwrap applies capability arguments in order, so these have to follow
	// the drop above, as sandbox.Args emits them.
	for _, c := range h.Caps {
		if !strings.HasPrefix(c, "CAP_") {
			return nil, fmt.Errorf("gateway: capability %q is missing the CAP_ prefix", c)
		}
		args = append(args, "--cap-add", c)
	}

	args = append(args,
		"--clearenv",

		// The host's own /proc, bound rather than mounted afresh, because
		// the pids a helper names are the host's and the paths under them
		// are only read.
		"--ro-bind", "/proc", "/proc",

		"--dev", "/dev",
		"--tmpfs", "/tmp",

		"--new-session",
	)

	for _, d := range h.Devices {
		args = append(args, "--dev-bind", d, d)
	}

	args = append(args, separator)

	return append(args, h.Args...), nil
}

// separator ends bwrap's own options and begins the command.
const separator = "--"

// spec renders the gateway into the sandbox it runs in.
func (g Gateway) spec(bundle images.Bundle, configPath string, creds *mtls.Credentials) sandbox.Spec {
	return sandbox.Spec{
		Rootfs:   bundle.Rootfs,
		Hostname: gatewayHostname,
		// Zero and not the image's uid, which is what every other
		// sandbox takes. bwrap builds a second user namespace to switch
		// to a non-zero uid, and the network namespace stays owned by
		// the first, so CAP_NET_ADMIN granted in the second does not
		// reach it. The gateway programs nftables in that namespace, and
		// with the image's uid every rule came back Operation not
		// permitted. Measured with NS_GET_USERNS on the sandbox's own
		// netns: at uid 0 the owner is the process's own user namespace,
		// and at any other uid the ioctl is refused outright because the
		// owner is a namespace the process is not in.
		//
		// It is not host root. It is uid 0 of a user namespace bwrap
		// created for this sandbox, mapped to the invoking user, holding
		// CAP_NET_ADMIN and nothing else.
		UID: 0,
		GID: 0,

		// Its own empty namespace. The uplink and every workload's veth are
		// put into it from the session namespace above rather than by
		// anything in here, so the gateway holds nothing over a sibling
		// sandbox's namespace.
		Net: sandbox.NetNone,

		Seccomp: true,

		// The gateway programs nftables at startup. bwrap holds the grant in
		// the user namespace it creates for this sandbox, so it reaches that
		// namespace and its descendants and nothing on the host.
		//
		// DisableUserns is left off, as it is for a workload. The gateway
		// itself has no use for a nested namespace, but turning the flag on
		// for the one sandbox every other launch waits for is a change to
		// make on a host where it can be measured.
		CapsAdd: []string{"CAP_NET_ADMIN"},

		Mounts: []sandbox.Mount{
			{Src: configPath, Dst: inConfigPath, ReadOnly: true},
			{Src: g.SecretsDir, Dst: inSecretsDir, ReadOnly: true},
			// The directory and not the socket: the socket does not exist
			// yet, the gateway creates it. Writable for the same reason.
			{Src: g.SocketDir, Dst: inSocketDir},
		},

		Env:  g.env(bundle, creds),
		Args: []string{gatewayCommand},
		Cwd:  bundle.Cwd,
	}
}

// env is the whole environment of the gateway process. bwrap clears the
// environment, so the image's own entries are part of this list rather than
// something a runtime applies underneath it.
func (g Gateway) env(bundle images.Bundle, creds *mtls.Credentials) []string {
	const extra = 6

	env := make([]string, 0, len(bundle.Env)+extra)
	env = append(env, bundle.Env...)

	return append(env,
		"CONFIG_PATH="+inConfigPath,
		"SECRETS_DIR="+inSecretsDir,
		"CONTROL_SOCKET="+g.inSocket(),

		// The server half of the control credentials, which exists here and
		// in no file. PackArgs keeps it off the command line, which is the
		// same reason a mime enabled workload's key travels that way.
		"Q_MTLS_CA="+string(creds.CA),
		"Q_MTLS_CERT="+string(creds.ServerPEM),
		"Q_MTLS_KEY="+string(creds.ServerKeyPEM),
	)
}

// inSocket is the control socket's path inside the sandbox. The name is taken
// from the host path so the two cannot drift.
func (g Gateway) inSocket() string {
	return filepath.Join(inSocketDir, filepath.Base(g.Socket))
}

// ready waits for the gateway to report that its DNS resolver, its proxy and
// its netfilter ruleset are all up.
//
// Readiness is this call and nothing else. A socket on disk says only that a
// listener bound, which happens before netfilter is programmed, and a
// workload started in that window would have egress with no rules on it.
func (g Gateway) ready() error {
	c, err := g.Client()
	if err != nil {
		return err
	}

	if err := c.Ready(context.Background()); err != nil {
		return err
	}

	slog.Debug("[gateway] the session gateway is ready")

	return nil
}

// reap waits for the gateway sandbox to exit and clears its state file.
//
// It runs in a goroutine because the launch has already returned. A qubesome
// run at a terminal exits long before the gateway does, so this often does
// not outlive the process it was started in, and the state file is left
// naming a pid that is gone. That is what sandbox.Alive is for: it records
// the start time too, so a file left behind reads as not running.
func reap(cmd *execabs.Cmd, statePath string) {
	if err := cmd.Wait(); err != nil {
		slog.Debug("the gateway sandbox exited", "path", statePath, "error", err)
	}

	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to remove the gateway state", "path", statePath, "error", err)
	}
}

// clientCreds is the client half of the control channel's mTLS material.
//
// It is written to disk because the gateway outlives the launch that started
// it and a later qubesome run has to reach the same gateway. The server half
// is not, and never leaves the gateway's environment.
type clientCreds struct {
	CA   []byte `json:"ca"`
	Cert []byte `json:"cert"`
	Key  []byte `json:"key"`
}

func (g Gateway) writeCreds(creds *mtls.Credentials) error {
	data, err := json.Marshal(clientCreds{
		CA:   creds.CA,
		Cert: creds.ClientPEM,
		Key:  creds.ClientKeyPEM,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal the gateway control credentials: %w", err)
	}

	if err := os.WriteFile(g.CredsPath, data, files.FileMode); err != nil {
		return fmt.Errorf("failed to write the gateway control credentials %q: %w", g.CredsPath, err)
	}

	return nil
}

func (g Gateway) readCreds() (clientCreds, error) {
	data, err := os.ReadFile(g.CredsPath)
	if err != nil {
		return clientCreds{}, fmt.Errorf("failed to read the gateway control credentials %q: %w", g.CredsPath, err)
	}

	var c clientCreds
	if err := json.Unmarshal(data, &c); err != nil {
		return clientCreds{}, fmt.Errorf("failed to parse the gateway control credentials %q: %w", g.CredsPath, err)
	}

	return c, nil
}

// allocation records how much of the gateway's subnet has been handed out.
type allocation struct {
	// Subnet is the range the count belongs to. A launch asking against a
	// different one is refused rather than counted, because the running
	// gateway's own end of every veth is in the recorded range.
	Subnet string `json:"subnet"`

	// Allocated is how many workload addresses have been handed out.
	Allocated uint64 `json:"allocated"`
}

// GatewayAddr returns the address the gateway holds on every veth, which is
// the first host address of the subnet. Workloads take the addresses above
// it.
func GatewayAddr(subnet netip.Prefix) (netip.Addr, error) {
	return addrAt(subnet, 1)
}

// Allocate returns the next address for a workload on the gateway's subnet.
//
// The count only ever goes up, so no address is handed out twice while the
// gateway that was told about it is running. That is what makes the gateway's
// map safe with nothing expiring entries out of it. The failure an expiry
// would have bounded, a workload inheriting the credentials injected for
// whoever held its address before, needs an address to come round again, and
// none does. It is also why Unregister can be best effort: a launch at a
// terminal exits before its workload, so no Unregister arrives, and what that
// leaks is a map entry naming an address nothing will be given again.
//
// The count is reset by starting a gateway and by nothing else. A gateway
// that has gone took its map with it, and the kernel took every veth to it
// when its network namespace went, so the addresses the old one knew about
// mean nothing to the one that replaces it.
//
// The gateway's own lock covers the read and the write together, so two
// launches at once cannot both act on the same count. flock is held on the
// open file description rather than by the process, so this serialises two
// launches inside one process as well as two processes.
func (g Gateway) Allocate(subnet netip.Prefix) (netip.Addr, error) {
	// Both are already true of a subnet that came through
	// types.GatewayConfig.SubnetPrefix. They are checked again because the
	// arithmetic below is wrong rather than merely unhelpful without them.
	if !subnet.Addr().Is4() {
		return netip.Addr{}, fmt.Errorf("the gateway subnet %s is not IPv4", subnet)
	}
	if subnet.Bits() > 30 {
		return netip.Addr{}, fmt.Errorf("the gateway subnet %s has no host addresses", subnet)
	}

	lock, err := acquire(g.LockPath)
	if err != nil {
		return netip.Addr{}, err
	}
	defer lock.Close()

	a, err := g.readAlloc(subnet)
	if err != nil {
		return netip.Addr{}, err
	}

	// The network address, the gateway's own and the broadcast address are
	// none of them a workload's to take, so a /24 gives 253 launches. A
	// deployment that needs more configures a shorter prefix.
	const reserved = 3

	capacity := uint64(1)<<(32-subnet.Bits()) - reserved
	if a.Allocated >= capacity {
		return netip.Addr{}, fmt.Errorf(
			"the gateway subnet %s is exhausted: all %d of its addresses have been handed out this session",
			subnet, capacity)
	}

	// The first workload takes the address above the gateway's.
	addr, err := addrAt(subnet, a.Allocated+2)
	if err != nil {
		return netip.Addr{}, err
	}

	a.Allocated++
	if err := g.writeAlloc(a); err != nil {
		return netip.Addr{}, err
	}

	slog.Debug("[gateway] allocated a workload address", "address", addr, "subnet", subnet)

	return addr, nil
}

// readAlloc returns the count so far. A file that is not there is a gateway
// that has handed nothing out yet, which is what a freshly started one looks
// like.
func (g Gateway) readAlloc(subnet netip.Prefix) (allocation, error) {
	data, err := os.ReadFile(g.AllocPath)
	if errors.Is(err, os.ErrNotExist) {
		return allocation{Subnet: subnet.String()}, nil
	}
	if err != nil {
		return allocation{}, fmt.Errorf("failed to read the gateway addresses %q: %w", g.AllocPath, err)
	}

	var a allocation
	if err := json.Unmarshal(data, &a); err != nil {
		return allocation{}, fmt.Errorf("failed to parse the gateway addresses %q: %w", g.AllocPath, err)
	}

	if a.Subnet != subnet.String() {
		return allocation{}, fmt.Errorf(
			"this session's gateway hands addresses out of %s and the config now asks for %s: "+
				"the running gateway holds the first address of the old range, so the session has to be restarted",
			a.Subnet, subnet)
	}

	return a, nil
}

func (g Gateway) writeAlloc(a allocation) error {
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("failed to marshal the gateway addresses: %w", err)
	}

	if err := os.WriteFile(g.AllocPath, data, files.FileMode); err != nil {
		return fmt.Errorf("failed to write the gateway addresses %q: %w", g.AllocPath, err)
	}

	return nil
}

// addrAt returns the address offset positions into subnet.
func addrAt(subnet netip.Prefix, offset uint64) (netip.Addr, error) {
	if !subnet.Addr().Is4() {
		return netip.Addr{}, fmt.Errorf("the gateway subnet %s is not IPv4", subnet)
	}

	b := subnet.Masked().Addr().As4()

	v := uint64(binary.BigEndian.Uint32(b[:])) + offset
	if v > math.MaxUint32 {
		return netip.Addr{}, fmt.Errorf("address %d of the gateway subnet %s is past the end of IPv4", offset, subnet)
	}

	var out [4]byte
	binary.BigEndian.PutUint32(out[:], uint32(v))

	addr := netip.AddrFrom4(out)
	if !subnet.Contains(addr) {
		return netip.Addr{}, fmt.Errorf("address %s is outside the gateway subnet %s", addr, subnet)
	}

	return addr, nil
}
