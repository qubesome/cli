package gateway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"syscall"

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
)

// The descriptors the sandbox is handed, in the order they are put in
// exec.Cmd.ExtraFiles, which os/exec numbers from 3 upwards. The seccomp
// filter and the packed arguments are read by the inner bwrap and the
// namespace by the outer one, and all three pass through the outer bwrap
// unchanged.
const (
	seccompFD = 3
	packedFD  = 4
	usernsFD  = 5
)

// Serves reports whether network names a network the gateway provides.
//
// An empty value, none and host all mean something to a sandbox with no
// gateway, and none of them is a request for one. Anything else names a
// network only the gateway can create.
func Serves(network string) bool {
	switch network {
	case "", "none", "host":
		return false
	default:
		return true
	}
}

// Ensure starts the session's gateway when the launch needs one, and returns
// once it reports itself ready.
//
// It fails closed, and that is the one behaviour here worth being rigid
// about. A configured gateway that will not start is an error that stops the
// workload. Not a warning, not a degraded launch, not egress without rules on
// it. A workload whose policy says which hosts it may reach must never run in
// a state where that policy is not being applied.
//
// An absent gateway block is not a failure. It means no gateway and no egress
// for anything, which is a supported configuration, so the rule binds only
// those who asked for a gateway. A workload that asks for no network is not a
// failure either: it gets nothing, which is the same thing the rule protects.
func Ensure(cfg *types.Config, network string) error {
	if cfg == nil || cfg.Gateway == nil {
		return nil
	}
	if !Serves(network) {
		return nil
	}

	return Current().Up(*cfg.Gateway, cfg.RootDir)
}

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

	return g.launch(g.spec(bundle, configPath, creds))
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

// launch starts the gateway sandbox and records it.
func (g Gateway) launch(spec sandbox.Spec) error {
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
	outer, packed, err := sandbox.PackArgs(spec, args, packedFD)
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

	cmd := execabs.Command(files.BwrapBinary, ns.Enter(outer)...) //nolint:gosec // the arguments are built from the gateway config.
	cmd.ExtraFiles = []*os.File{filter, packed, ns.File()}

	// The gateway outlives the launch that started it, and a qubesome run
	// typed at a terminal sits in the shell's foreground process group. A
	// session whose gateway ended at the first Ctrl-C would take the egress
	// of every workload still running with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start the gateway sandbox: %w", err)
	}

	if err := sandbox.WriteState(g.StatePath, cmd.Process.Pid); err != nil {
		// A gateway nothing can find again is worse than none: the next
		// launch would start a second one on the same socket.
		if kerr := cmd.Process.Kill(); kerr != nil {
			slog.Warn("failed to kill the unrecorded gateway", "error", kerr)
		}
		_ = cmd.Wait()

		return fmt.Errorf("failed to record the gateway state: %w", err)
	}

	// This gateway holds an empty map, so the addresses its predecessor was
	// told about mean nothing to it and the count starts again.
	if err := os.Remove(g.AllocPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to reset the gateway addresses", "path", g.AllocPath, "error", err)
	}

	slog.Debug("[gateway] started the session gateway", "pid", cmd.Process.Pid)

	go reap(cmd, g.StatePath)

	return nil
}

// spec renders the gateway into the sandbox it runs in.
func (g Gateway) spec(bundle images.Bundle, configPath string, creds *mtls.Credentials) sandbox.Spec {
	return sandbox.Spec{
		Rootfs:   bundle.Rootfs,
		Hostname: gatewayHostname,
		UID:      bundle.UID,
		GID:      bundle.GID,

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
