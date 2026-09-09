package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// What is covered here is the part of the guest init that is a value
// rather than a machine: the configuration the host composed into the
// image, the table of filesystems the guest is given, the device nodes
// behind the devtmpfs fallback, and the delivery of an exit status from
// the reaper to whoever asked for it.
//
// Nothing else in vminit_linux.go can run in this container, and not for
// want of a tool. mount, mknod, sethostname and reboot act on the machine
// the process is in, and the machine here is the development container:
// the mounts would land on its own /proc and /dev, the device nodes need
// a privilege a user namespace does not carry, and the reboot, were it
// permitted, would take the host down with it. A wait4(-1) loop is worse
// than untestable here, since it would collect the children the rest of
// this package's tests are waiting for by pid, which is the exact race
// the guest reaper exists to avoid. All of it is exercised on a booted VM
// by the acceptance pass instead.

func TestDecodeVMConfig(t *testing.T) {
	t.Parallel()

	cfg, err := decodeVMConfig([]byte(`{
		"argv": ["/bin/bash", "-l"],
		"env": ["PATH=/usr/bin", "HOME=/root"],
		"cwd": "/root",
		"hostname": "dev-personal",
		"dataMount": "/data"
	}`))
	require.NoError(t, err)

	assert.Equal(t, []string{"/bin/bash", "-l"}, cfg.Argv)
	assert.Equal(t, []string{"PATH=/usr/bin", "HOME=/root"}, cfg.Env)
	assert.Equal(t, "/root", cfg.Cwd)
	assert.Equal(t, "dev-personal", cfg.Hostname)
	assert.Equal(t, "/data", cfg.DataMount)
}

// The host leaves dataMount out when the workload configured no disk, and
// an absent mount point is how the guest is told there is no second drive
// to mount.
func TestDecodeVMConfigWithoutADataDisk(t *testing.T) {
	t.Parallel()

	cfg, err := decodeVMConfig([]byte(`{"argv": ["/bin/sh"], "hostname": "dev"}`))
	require.NoError(t, err)

	assert.Empty(t, cfg.DataMount)
	assert.Empty(t, dataMountTable(cfg.DataMount))
}

func TestDecodeVMConfigRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]string{
		"nothing at all":     "",
		"truncated":          `{"argv": ["/bin/sh"`,
		"an array":           `["/bin/sh"]`,
		"argv as a string":   `{"argv": "/bin/sh"}`,
		"more after the end": `{"argv": ["/bin/sh"]} and then some`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeVMConfig([]byte(in))
			require.Error(t, err)
		})
	}
}

func TestReadVMConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "init.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"argv": ["/bin/sh"]}`), 0o600))

	cfg, err := readVMConfig(path)
	require.NoError(t, err)

	assert.Equal(t, []string{"/bin/sh"}, cfg.Argv)
}

// A guest whose image carries no init.json has nothing to run and no way
// to find out what it should have been, so the file not being there is
// reported as itself rather than as an empty configuration.
func TestReadVMConfigWithoutTheFile(t *testing.T) {
	t.Parallel()

	_, err := readVMConfig(filepath.Join(t.TempDir(), "init.json"))
	require.Error(t, err)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadVMConfigThatDoesNotDecode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "init.json")
	require.NoError(t, os.WriteFile(path, []byte("argv=/bin/sh"), 0o600))

	_, err := readVMConfig(path)
	require.Error(t, err)
	assert.NotErrorIs(t, err, os.ErrNotExist)
}

// The order is the part of the table that fails quietly if it is wrong.
// /dev/pts and /dev/shm mounted before /dev land on the image's own empty
// directories and are hidden the moment /dev goes over them.
func TestMountTableIsInTheOrderTheMountsDependOn(t *testing.T) {
	t.Parallel()

	table := mountTable()

	targets := make([]string, 0, len(table))
	for _, m := range table {
		targets = append(targets, m.Target)
	}

	assert.Equal(t, []string{"/proc", "/sys", "/dev", "/dev/pts", "/dev/shm", "/tmp", "/run"}, targets)
}

func TestMountTableFlags(t *testing.T) {
	t.Parallel()

	for _, m := range mountTable() {
		assert.NotZero(t, m.Flags&unix.MS_NOSUID,
			"%s is a filesystem the init makes, so nothing on it was ever meant to be setuid", m.Target)

		// /dev holds device nodes and /dev/pts is nothing but device
		// nodes, and MS_NODEV is what stops one being opened.
		if m.Target == devDir || m.Target == "/dev/pts" {
			assert.Zero(t, m.Flags&unix.MS_NODEV, "%s has to allow the device nodes on it to be opened", m.Target)
			continue
		}

		assert.NotZero(t, m.Flags&unix.MS_NODEV, "%s has no device nodes on it", m.Target)
	}
}

func TestMountTableFilesystems(t *testing.T) {
	t.Parallel()

	got := make(map[string]string, len(mountTable()))
	for _, m := range mountTable() {
		got[m.Target] = m.FSType
	}

	assert.Equal(t, map[string]string{
		"/proc":    "proc",
		"/sys":     "sysfs",
		"/dev":     "devtmpfs",
		"/dev/pts": "devpts",
		"/dev/shm": "tmpfs",
		"/tmp":     "tmpfs",
		"/run":     "tmpfs",
	}, got)
}

// A console is allocated through /dev/ptmx, and without ptmxmode it can
// only be opened by root.
func TestMountTableUnlocksPtmx(t *testing.T) {
	t.Parallel()

	for _, m := range mountTable() {
		if m.FSType == "devpts" {
			assert.Contains(t, m.Data, "ptmxmode=0666")
			return
		}
	}

	t.Fatal("the table has no devpts, so there is nowhere for a console to be allocated")
}

func TestDataMountTable(t *testing.T) {
	t.Parallel()

	table := dataMountTable("/data")
	require.Len(t, table, 1)

	assert.Equal(t, dataDisk, table[0].Source)
	assert.Equal(t, "/data", table[0].Target)
	assert.Equal(t, "ext4", table[0].FSType)
}

// The mount point is what tells the guest there is a disk at all, so an
// empty one has to produce no mount rather than a mount of /dev/vdb over
// the root of the machine.
func TestDataMountTableWithoutAMountPoint(t *testing.T) {
	t.Parallel()

	assert.Empty(t, dataMountTable(""))
}

// The fallback has to land on the mount point the devtmpfs attempt failed
// on, because that is what mountAll is keyed on.
func TestDevTmpfsFallbackReplacesDev(t *testing.T) {
	t.Parallel()

	fallback := devTmpfs()

	assert.Equal(t, devDir, fallback.Target)
	assert.Equal(t, "tmpfs", fallback.FSType)
	assert.NotEqual(t, "devtmpfs", fallback.FSType, "the fallback exists for a kernel that has no devtmpfs")
}

func TestDevNodes(t *testing.T) {
	t.Parallel()

	nodes := devNodes()

	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		names = append(names, n.Name)
	}

	assert.Equal(t, []string{
		guestConsole,
		"/dev/null",
		"/dev/zero",
		"/dev/urandom",
		"/dev/ptmx",
		rootDisk,
		dataDisk,
	}, names)
}

// A disk made as a character device is one the mount fails on, with
// nothing in the failure pointing back at this list.
func TestDevNodesGiveTheDisksBlockDevices(t *testing.T) {
	t.Parallel()

	for _, n := range devNodes() {
		want := uint32(unix.S_IFCHR)
		if n.Name == rootDisk || n.Name == dataDisk {
			want = unix.S_IFBLK
		}

		assert.Equal(t, want, n.Mode&unix.S_IFMT, n.Name)
		assert.NotZero(t, n.Mode&0o777, "%s would be a node nothing can open", n.Name)
	}
}

// A wait status is the kernel's encoding, with the exit code in the
// second byte and a terminating signal in the low seven bits. The reaper
// hands one over as it collected it, so this is what a waiter has to make
// sense of.
func TestGuestChildReportsHowTheProcessEnded(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status unix.WaitStatus
		wantOK bool
		want   string
	}{
		"exited cleanly": {status: unix.WaitStatus(0), wantOK: true},
		"exited with a status": {
			status: unix.WaitStatus(3 << 8),
			want:   "exited with status 3",
		},
		"killed": {
			status: unix.WaitStatus(uint32(unix.SIGKILL)),
			want:   "ended with signal: killed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := make(chan unix.WaitStatus, 1)
			status <- tc.status

			err := (&guestChild{name: "/bin/sh", status: status}).Wait()
			if tc.wantOK {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestGuestReaperDeliversToTheWaiter(t *testing.T) {
	t.Parallel()

	r := &guestReaper{children: map[int]chan unix.WaitStatus{}}

	status := make(chan unix.WaitStatus, 1)
	r.children[42] = status

	r.deliver(42, unix.WaitStatus(7<<8))

	err := (&guestChild{name: "/bin/sh", status: status}).Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "7")
}

// Everything orphaned anywhere in the machine reparents to pid 1, so most
// of what the loop collects belongs to nobody. Collecting it is the whole
// job, and the loop must not block handing it on.
func TestGuestReaperDropsAnOrphan(t *testing.T) {
	t.Parallel()

	r := &guestReaper{children: map[int]chan unix.WaitStatus{}}

	r.deliver(42, unix.WaitStatus(0))

	assert.Empty(t, r.children)
}

// The kernel reuses pids. An entry left behind after a status was
// delivered would hand the next process to take that pid to a waiter that
// finished with it long ago.
func TestGuestReaperForgetsAPidItDelivered(t *testing.T) {
	t.Parallel()

	r := &guestReaper{children: map[int]chan unix.WaitStatus{}}
	r.children[42] = make(chan unix.WaitStatus, 1)

	r.deliver(42, unix.WaitStatus(0))

	assert.NotContains(t, r.children, 42)
}
