//go:build linux

package sandbox

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// The guest listener needs a kernel with vsock support, which a container
// does not have. These run in a VM, and Task 10 is where that happens.
func requireVSOCK(t *testing.T) {
	t.Helper()

	if _, err := os.Stat("/dev/vsock"); err != nil {
		t.Skipf("no vsock support in this kernel: %v", err)
	}
}

// dialVSOCKLocal reaches a listener on this machine over the loopback
// context id. The host never does this, since it goes through firecracker
// instead, so it exists only to exercise the guest side from a test.
func dialVSOCKLocal(t *testing.T, port uint32) (net.Conn, error) {
	t.Helper()

	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	if err := unix.Connect(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_LOCAL, Port: port}); err != nil {
		_ = unix.Close(fd)

		return nil, err
	}

	// The descriptor connects blocking and is made non-blocking before
	// os.NewFile sees it, because that is what the poller adopts and the
	// deadlines the exchange sets need.
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)

		return nil, err
	}

	return &vsockConn{File: os.NewFile(uintptr(fd), "vsock"), addr: vsockAddr("vsock")}, nil
}

func TestVSOCKListenerServesTheSameExchange(t *testing.T) {
	t.Parallel()
	requireVSOCK(t)

	const port = 51234

	ln, err := listenVSOCK(port)
	require.NoError(t, err)

	t.Cleanup(func() { _ = ln.Close() })

	assert.Equal(t, "vsock", ln.Addr().Network())

	s := &supervisor{}
	go s.serve(ln)

	conn, err := dialVSOCKLocal(t, port)
	if err != nil {
		t.Skipf("no vsock loopback on this machine: %v", err)
	}

	out := filepath.Join(t.TempDir(), "out")

	require.NoError(t, spawn(conn, []string{"/bin/sh", "-c", "printf %s \"$0\" > " + out, "ran"}))
	s.spawned.Wait()

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "ran", string(data))
}

// Accept waits on the poller, and a wait the listener's own Close cannot
// end is a supervisor that outlives its sandbox.
func TestVSOCKListenerStopsAccepting(t *testing.T) {
	t.Parallel()
	requireVSOCK(t)

	ln, err := listenVSOCK(51235)
	require.NoError(t, err)

	accepted := make(chan error, 1)

	go func() {
		_, err := ln.Accept()
		accepted <- err
	}()

	require.NoError(t, ln.Close())

	select {
	case err := <-accepted:
		require.Error(t, err)
	case <-time.After(exchangeTimeout):
		t.Fatal("Accept did not return after the listener was closed")
	}
}
