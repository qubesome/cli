package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// VMInitCommand is the qubesome subcommand that runs as a guest's init.
//
// It is named here because both ends need it. The cli registers a command
// under it, and a guest reaches that command through
// init=/sbin/qubesome-init on its kernel command line with this name as a
// bare word after it, since the kernel hands every word it does not
// recognise itself, and that is not a key=value, to init as an argument.
const VMInitCommand = "vm-init"

// VMSupervisorPort is the vsock port a guest's supervisor listens on.
//
// There is one machine per workload and one supervisor per machine, so
// there is nothing to allocate and a fixed number is what lets both ends
// agree without being told. 1024 is the first unprivileged vsock port.
const VMSupervisorPort uint32 = 1024

const (
	// guestInitConfig is the file the host composed into the image saying
	// what the guest runs. It is written by
	// internal/runners/firecracker/rootfs.go and the path is the
	// agreement between the two.
	guestInitConfig = "/etc/qubesome/init.json"

	// guestConsole is where everything a guest has to say goes. See
	// openConsole for why opening it is the init's job.
	guestConsole = "/dev/console"

	// devDir is the one mount point with a fallback behind it, and
	// mountAll is keyed on it.
	devDir = "/dev"

	// rootDisk and dataDisk are the virtio block devices firecracker
	// attaches, in that order. The first is the root filesystem the
	// kernel already mounted and the second is the persistent disk, which
	// is only there when the workload configured one.
	rootDisk = "/dev/vda"
	dataDisk = "/dev/vdb"

	// virtioBlkMajor is the block major virtio_blk is given. See devNodes
	// for why a fixed number is acceptable.
	virtioBlkMajor = 254
)

// vmConfig is what the guest init is given, read from guestInitConfig.
//
// It is the reading half of InitConfig in
// internal/runners/firecracker/rootfs.go, which is where the fields are
// described. The shape is written out twice rather than shared because
// the firecracker runner already depends on this package, and importing
// it back would close the loop.
type vmConfig struct {
	Argv     []string `json:"argv"`
	Env      []string `json:"env"`
	Cwd      string   `json:"cwd"`
	Hostname string   `json:"hostname"`

	// DataMount is where the persistent disk goes, and is empty when the
	// workload configured none.
	DataMount string `json:"dataMount,omitempty"`
}

// readVMConfig reads what the guest was booted to run.
//
// A file that is not there or does not decode is a hard error and not a
// set of defaults, because there is nothing else in the machine to run
// and no way to guess what was meant. All that is left is to say so on
// the console.
func readVMConfig(path string) (vmConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return vmConfig{}, fmt.Errorf("sandbox: failed to read the guest init configuration: %w", err)
	}

	return decodeVMConfig(data)
}

func decodeVMConfig(data []byte) (vmConfig, error) {
	var cfg vmConfig

	if err := json.Unmarshal(data, &cfg); err != nil {
		return vmConfig{}, fmt.Errorf("sandbox: failed to decode the guest init configuration: %w", err)
	}

	return cfg, nil
}

// mount is one filesystem the guest init makes.
type mount struct {
	Source string
	Target string
	FSType string
	Flags  uintptr
	Data   string
}

// mountTable is every filesystem the machine needs before anything runs
// in it, in the order it has to be made.
//
// None of it comes from the image. An OCI rootfs carries no /proc, no
// device nodes and frequently no /run either, because a container runtime
// supplies all three, and in a machine the init is the runtime.
//
// The order carries meaning. /dev has to be mounted before /dev/pts and
// /dev/shm can be mounted under it, and /proc has to be there before
// anything in the image goes looking for it. The persistent disk is not
// in here because it is the one mount whose target comes from the
// configuration, which is read after these are up. See dataMountTable.
func mountTable() []mount {
	const noSuidDevExec = unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC

	return []mount{
		{Source: "proc", Target: "/proc", FSType: "proc", Flags: noSuidDevExec},
		{Source: "sysfs", Target: "/sys", FSType: "sysfs", Flags: noSuidDevExec},

		// MS_NODEV is deliberately absent, since device nodes are the
		// whole point of this one.
		{Source: "devtmpfs", Target: devDir, FSType: "devtmpfs", Flags: unix.MS_NOSUID, Data: "mode=0755"},

		// gid 5 is the tty group on nearly every image, and ptmxmode is
		// what makes /dev/ptmx usable by a process that is not root. The
		// console a workload is attached to is allocated through it.
		{
			Source: "devpts", Target: "/dev/pts", FSType: "devpts",
			Flags: unix.MS_NOSUID | unix.MS_NOEXEC,
			Data:  "gid=5,mode=0620,ptmxmode=0666",
		},

		{Source: "tmpfs", Target: "/dev/shm", FSType: "tmpfs", Flags: unix.MS_NOSUID | unix.MS_NODEV, Data: "mode=1777"},
		{Source: "tmpfs", Target: "/tmp", FSType: "tmpfs", Flags: unix.MS_NOSUID | unix.MS_NODEV, Data: "mode=1777"},
		{Source: "tmpfs", Target: "/run", FSType: "tmpfs", Flags: unix.MS_NOSUID | unix.MS_NODEV, Data: "mode=0755"},
	}
}

// dataMountTable is the persistent disk, or nothing when the workload
// configured none.
//
// The host attaches /dev/vdb exactly when it configured a mount point for
// it, so the mount point is also the answer to whether the device is
// there. A disk that was attached and does not mount fails the boot,
// rather than leaving a machine running whose persistent data silently is
// not in it.
func dataMountTable(target string) []mount {
	if target == "" {
		return nil
	}

	return []mount{
		{Source: dataDisk, Target: target, FSType: "ext4", Flags: unix.MS_NOSUID | unix.MS_NODEV},
	}
}

// devTmpfs is what /dev becomes when the kernel has no devtmpfs.
func devTmpfs() mount {
	return mount{Source: "tmpfs", Target: devDir, FSType: "tmpfs", Flags: unix.MS_NOSUID, Data: "mode=0755"}
}

// devNode is one device a fallback /dev is given.
type devNode struct {
	Name  string
	Mode  uint32
	Major uint32
	Minor uint32
}

// devNodes is the fixed list a tmpfs /dev is filled with.
//
// Whether the pinned kernel is built with CONFIG_DEVTMPFS could not be
// answered from the host, and this is the answer to it being no. It is a
// short fixed list rather than anything cleverer because the alternative
// to guessing is a machine that boots to a blank console with nothing on
// it to say why. These seven are what makes a machine usable: a console
// to speak on, the three character devices every program assumes, the pty
// multiplexor a console is allocated through, and the two disks.
//
// The virtio block major is not fixed by the kernel. virtio_blk asks for
// a dynamically allocated one, and a firecracker guest has few enough
// block drivers that it is the first handed out, which is 254. A kernel
// with devtmpfs never reaches this, and that is the usual case, which is
// what makes a guess here acceptable where it would not be elsewhere.
func devNodes() []devNode {
	return []devNode{
		{Name: guestConsole, Mode: unix.S_IFCHR | 0o600, Major: 5, Minor: 1},
		{Name: "/dev/null", Mode: unix.S_IFCHR | 0o666, Major: 1, Minor: 3},
		{Name: "/dev/zero", Mode: unix.S_IFCHR | 0o666, Major: 1, Minor: 5},
		{Name: "/dev/urandom", Mode: unix.S_IFCHR | 0o666, Major: 1, Minor: 9},
		{Name: "/dev/ptmx", Mode: unix.S_IFCHR | 0o666, Major: 5, Minor: 2},
		{Name: rootDisk, Mode: unix.S_IFBLK | 0o660, Major: virtioBlkMajor, Minor: 0},
		{Name: dataDisk, Mode: unix.S_IFBLK | 0o660, Major: virtioBlkMajor, Minor: 16},
	}
}

// mountAll makes every filesystem in the table, and /dev by hand when the
// kernel cannot make it.
func mountAll(table []mount) error {
	for _, m := range table {
		err := mountOne(m)
		if err == nil {
			continue
		}

		if m.Target != devDir {
			return err
		}

		slog.Warn("the guest kernel has no devtmpfs, filling a tmpfs /dev instead", "error", err)

		if err := mountOne(devTmpfs()); err != nil {
			return err
		}

		if err := makeDevNodes(devNodes()); err != nil {
			return err
		}
	}

	return nil
}

// mountOne makes one filesystem, creating its mount point first. The
// image is not required to have any of these directories and several do
// not. The mode the directory is created with does not outlive the mount,
// since what is visible afterwards is the mounted filesystem's own.
func mountOne(m mount) error {
	if err := os.MkdirAll(m.Target, 0o755); err != nil {
		return fmt.Errorf("sandbox: failed to create the mount point %q: %w", m.Target, err)
	}

	if err := unix.Mount(m.Source, m.Target, m.FSType, m.Flags, m.Data); err != nil {
		return fmt.Errorf("sandbox: failed to mount %s on %q: %w", m.FSType, m.Target, err)
	}

	return nil
}

// makeDevNodes creates the device nodes a fallback /dev needs. A node the
// image already carries is left alone, because the image's own is at
// least as likely to be right as this list is.
func makeDevNodes(nodes []devNode) error {
	for _, n := range nodes {
		dev := int(unix.Mkdev(n.Major, n.Minor)) //nolint:gosec // G115: the pairs are the fixed constants in devNodes.

		err := unix.Mknod(n.Name, n.Mode, dev)
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("sandbox: failed to create the device node %q: %w", n.Name, err)
		}
	}

	return nil
}

// openConsole gives the init standard streams.
//
// The kernel opens /dev/console for init and puts it on the first three
// descriptors, but only if the node is in the root filesystem when it
// mounts it, and an OCI image ships no device nodes at all. So init
// starts with nothing open on 0, 1 and 2: every message it writes goes
// nowhere, and worse, the first files it opens land on those numbers and
// are inherited as the standard streams of everything it starts. The node
// is there once /dev is mounted, which is why this runs straight after.
//
// A machine with no console node at all is not refused, because a console
// attached over vsock brings a pty of its own and the machine is still
// usable through one. What must not be left is a descriptor of the three
// closed, so the null device stands in.
func openConsole() error {
	fd, err := unix.Open(guestConsole, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		slog.Warn("the guest has no console, so nothing it writes will be seen", "error", err)

		fd, err = unix.Open(os.DevNull, unix.O_RDWR, 0)
		if err != nil {
			return fmt.Errorf("sandbox: the guest has neither a console nor a null device: %w", err)
		}
	}

	for _, target := range []int{0, 1, 2} {
		// The open lands on the lowest free descriptor, so when all
		// three were closed it is 0 itself, and dup3 refuses to
		// duplicate a descriptor onto the one it already is.
		if fd == target {
			continue
		}

		if err := unix.Dup3(fd, target, 0); err != nil {
			return fmt.Errorf("sandbox: failed to put the console on descriptor %d: %w", target, err)
		}
	}

	if fd > 2 {
		_ = unix.Close(fd)
	}

	return nil
}

// applyIdentity puts the machine into the state the workload described.
//
// The environment and the working directory are applied to the init
// rather than to the command it starts, so that everything started later
// inherits them too, including a sibling spawned into the machine long
// after it booted.
//
// The kernel's own environment is left underneath rather than cleared. It
// is two or three variables, PATH among them, and an image that declares
// none of its own is better off with those than with nothing.
func applyIdentity(cfg vmConfig) error {
	if cfg.Hostname != "" {
		if err := unix.Sethostname([]byte(cfg.Hostname)); err != nil {
			return fmt.Errorf("sandbox: failed to set the guest hostname to %q: %w", cfg.Hostname, err)
		}
	}

	for _, v := range cfg.Env {
		name, value, ok := strings.Cut(v, "=")
		if !ok || name == "" {
			slog.Warn("the guest init configuration carries an environment entry that is not a pair", "entry", v)
			continue
		}

		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("sandbox: failed to set %s in the guest environment: %w", name, err)
		}
	}

	if cfg.Cwd != "" {
		// A container runtime creates the working directory an image
		// declares and does not carry, and images that declare one they
		// never created are common enough to be worth matching. Without
		// this a machine would refuse to boot over a directory that is
		// not there.
		if err := os.MkdirAll(cfg.Cwd, 0o755); err != nil {
			return fmt.Errorf("sandbox: failed to create the working directory %q: %w", cfg.Cwd, err)
		}

		if err := os.Chdir(cfg.Cwd); err != nil {
			return fmt.Errorf("sandbox: failed to enter the working directory %q: %w", cfg.Cwd, err)
		}
	}

	return nil
}

// handleSignals gives pid 1 the dispositions it does not have.
//
// The kernel applies no default action to a signal pid 1 has installed no
// handler for, so SIGTERM and SIGINT are discarded rather than ending the
// process, and a machine asked to stop would only ever stop by being
// killed from the host. SIGINT is the one that matters most: firecracker
// asks a guest to shut down by sending it a ctrl alt del, and the kernel
// turns that into a SIGINT to pid 1.
//
// Both are answered by asking every process in the machine to stop.
// Signalling pid -1 reaches all of them and never the caller, so the main
// command is asked without this needing to know its pid, and the machine
// comes down the ordinary way: the main command exits, the supervisor
// returns and shutdown reboots.
func handleSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGTERM, unix.SIGINT)

	go func() {
		for sig := range ch {
			slog.Info("the guest was asked to stop", "signal", sig.String())

			if err := unix.Kill(-1, unix.SIGTERM); err != nil {
				slog.Error("failed to ask the guest's processes to stop", "error", err)
			}
		}
	}()
}

// guestReaper starts the machine's processes and owns every exit status
// in it.
//
// It exists because a supervisor in a VM is pid 1. Everything orphaned
// anywhere in the machine reparents here, so wait4(-1) has to be called
// or those orphans stay as zombies, and one wait4(-1) loop is the only
// arrangement that works. os/exec waits for a specific pid, and a reaper
// running alongside it collects that pid often enough first that Cmd.Wait
// answers ECHILD instead of a status. So nothing on this path ever calls
// Cmd.Wait. The loop collects everything, delivers the statuses that
// belong to a process it started to whoever is waiting for one, and drops
// the rest, which are the orphans, where they are collected.
//
// This is the opposite of the arrangement supervisor.reap describes, and
// the two must not be confused. Under bwrap a reaper sits above the
// supervisor and the supervisor waits for its own children. Here the
// supervisor is the reaper and can wait for nothing by pid.
type guestReaper struct {
	mu sync.Mutex

	// children maps the pid of a process this started to where its exit
	// status is delivered. A pid that is not in here is an orphan.
	children map[int]chan unix.WaitStatus
}

// startReaping begins collecting exit statuses.
//
// It has to be called before the first process is started, because the
// SIGCHLD the loop parks on below is only delivered once it has been
// asked for, and one raised before that is lost.
func startReaping() *guestReaper {
	r := &guestReaper{children: make(map[int]chan unix.WaitStatus)}

	// SIGCHLD is what wakes the loop when it has nothing left to wait
	// for. The channel is buffered, so a process that starts and exits
	// while the loop is between two calls still wakes it.
	sigchld := make(chan os.Signal, 1)
	signal.Notify(sigchld, unix.SIGCHLD)

	go r.collect(sigchld)

	return r
}

func (r *guestReaper) collect(sigchld <-chan os.Signal) {
	for {
		var ws unix.WaitStatus

		pid, err := unix.Wait4(-1, &ws, 0, nil)

		switch {
		case errors.Is(err, unix.EINTR):
			continue
		case errors.Is(err, unix.ECHILD):
			// There is nothing to wait for. wait4 answers that
			// immediately, so a loop around it would spin. It parks on
			// SIGCHLD instead, which the kernel raises for the next
			// process to change state.
			<-sigchld
			continue
		case err != nil:
			slog.Error("the guest stopped reaping", "error", err)
			return
		}

		r.deliver(pid, ws)
	}
}

func (r *guestReaper) deliver(pid int, ws unix.WaitStatus) {
	r.mu.Lock()
	status, ok := r.children[pid]
	delete(r.children, pid)
	r.mu.Unlock()

	if !ok {
		// An orphan that reparented here from somewhere in the machine.
		// Collecting it was the whole of the job and nothing is waiting
		// for the answer.
		slog.Debug("the guest reaped an orphan", "pid", pid)
		return
	}

	status <- ws
}

func (r *guestReaper) start(argv []string) (waiter, error) {
	cmd := command(argv)

	// Starting the process and registering its pid are one step. The
	// loop can collect a process that exits immediately before Start has
	// even returned, and a status collected for a pid that is not
	// registered yet would be dropped as an orphan's and never
	// delivered. deliver takes this same lock, so it waits here until
	// the pid it collected is in the map.
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: failed to start %q: %w", argv[0], err)
	}

	// The buffer is what keeps the loop from blocking on a caller that
	// has not asked for the status yet. cmd.Wait is deliberately never
	// called on this command, here or anywhere else.
	status := make(chan unix.WaitStatus, 1)
	r.children[cmd.Process.Pid] = status

	return &guestChild{name: argv[0], status: status}, nil
}

// guestChild waits for one process the reaper started.
type guestChild struct {
	name   string
	status <-chan unix.WaitStatus
}

// Wait blocks until the reaper delivers this process's exit status, and
// reports what os/exec's Wait would have, so that the supervisor above it
// cannot tell the two arrangements apart.
func (c *guestChild) Wait() error {
	ws := <-c.status

	switch {
	case ws.Signaled():
		return fmt.Errorf("sandbox: %q ended with signal: %s", c.name, ws.Signal())
	case ws.ExitStatus() != 0:
		return fmt.Errorf("sandbox: %q exited with status %d", c.name, ws.ExitStatus())
	}

	return nil
}

// VMInit runs the supervisor as the init of a microVM guest.
//
// It does not return while the machine is up, and when the machine comes
// down it does not return at all. See shutdown.
func VMInit() error {
	if err := vmInit(); err != nil {
		// The failure is reported rather than returned. Returning from
		// pid 1 panics the kernel, and a panic buries the reason under
		// the wrong process's stack trace. The console is the only place
		// a guest has to say anything and this is the last thing said
		// on it.
		slog.Error("the guest init failed", "error", err)
	}

	return shutdown()
}

func vmInit() error {
	// The filesystems come first, because everything after them needs
	// one, the console most of all. A failure here has nowhere to be
	// printed and the kernel's own panic is what the user is left with,
	// which is why the list is short and made only of what a kernel
	// always provides.
	if err := mountAll(mountTable()); err != nil {
		return err
	}

	if err := openConsole(); err != nil {
		return err
	}

	cfg, err := readVMConfig(guestInitConfig)
	if err != nil {
		return err
	}

	if err := mountAll(dataMountTable(cfg.DataMount)); err != nil {
		return err
	}

	if err := applyIdentity(cfg); err != nil {
		return err
	}

	handleSignals()

	return SuperviseVM(VMSupervisorPort, cfg.Argv)
}

// shutdown brings the machine down, and does not come back.
//
// unix.Sync comes first because a persistent disk is a real filesystem on
// a host file and the reboot does not write the page cache back for it.
// Then LINUX_REBOOT_CMD_RESTART, which with reboot=k on the kernel
// command line comes out as a write to the keyboard controller's reset
// line. That is what firecracker watches to know a machine is finished,
// and it is what makes reboot=k load-bearing rather than a leftover:
// without it the guest would try a reset method the VMM does not answer
// and the machine would stay up. panic=1 next to it covers the one
// failure this cannot, by rebooting a second after a kernel panic.
func shutdown() error {
	unix.Sync()

	if err := unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART); err != nil {
		return fmt.Errorf("sandbox: failed to shut the guest down: %w", err)
	}

	// Unreachable as pid 1 with CAP_SYS_BOOT, which is the only place
	// this runs.
	return nil
}
