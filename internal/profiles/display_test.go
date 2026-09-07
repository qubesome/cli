package profiles

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompositorArgs(t *testing.T) {
	t.Parallel()

	got, err := compositorArgs(DisplayParams{
		Geometry: "1920x1080",
	})
	require.NoError(t, err)

	require.Equal(t, []string{
		"--backend=x11",
		"--width=1920",
		"--height=1080",
		"--shell=kiosk-shell.so",
		"--socket=qubesome",
	}, got)
}

func TestCompositorArgsFullscreen(t *testing.T) {
	t.Parallel()

	got, err := compositorArgs(DisplayParams{
		Geometry:   "1920x1080",
		Fullscreen: true,
	})
	require.NoError(t, err)

	require.Equal(t, "--fullscreen", got[len(got)-1])
}

func TestCompositorArgsBadGeometry(t *testing.T) {
	t.Parallel()

	for _, g := range []string{"", "1920", "1920x", "x1080", "axb", "1920x1080x1"} {
		_, err := compositorArgs(DisplayParams{Geometry: g})
		require.Error(t, err, "geometry %q should be rejected", g)
	}
}

func TestXwaylandArgs(t *testing.T) {
	t.Parallel()

	base := DisplayParams{
		Display:       11,
		Geometry:      "1920x1080",
		AuthFile:      "/home/xorg-user/.Xserver",
		WindowManager: "exec dbus-run-session awesome",
	}

	tests := []struct {
		name string
		in   DisplayParams
		want []string
	}{
		{
			name: "defaults",
			in:   base,
			want: []string{
				":11",
				"-host-grab",
				"-geometry", "1920x1080",
				"-auth", "/home/xorg-user/.Xserver",
				"-extension", "MIT-SHM",
				"-extension", "XTEST",
				"-extension", "RECORD",
				"-nopn",
				"-tst",
				"-nolisten", "tcp",
				"--",
				"env", "-u", "WAYLAND_DISPLAY",
				"XDG_RUNTIME_DIR=/run/user/1000",
				"XAUTHORITY=/home/xorg-user/.Xauthority",
				"dbus-run-session", "awesome",
			},
		},
		{
			name: "extra args are appended before the separator",
			in: func() DisplayParams {
				p := base
				p.ExtraArgs = "-verbose 9"
				return p
			}(),
			want: []string{
				":11",
				"-host-grab",
				"-geometry", "1920x1080",
				"-auth", "/home/xorg-user/.Xserver",
				"-extension", "MIT-SHM",
				"-extension", "XTEST",
				"-extension", "RECORD",
				"-nopn",
				"-tst",
				"-nolisten", "tcp",
				"-verbose", "9",
				"--",
				"env", "-u", "WAYLAND_DISPLAY",
				"XDG_RUNTIME_DIR=/run/user/1000",
				"XAUTHORITY=/home/xorg-user/.Xauthority",
				"dbus-run-session", "awesome",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := xwaylandArgs(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestXwaylandArgsRejectsEmptyWindowManager(t *testing.T) {
	t.Parallel()

	_, err := xwaylandArgs(DisplayParams{
		Display:       11,
		Geometry:      "1920x1080",
		WindowManager: "exec ",
	})
	require.Error(t, err)
}

func TestXwaylandArgsDisablesEavesdroppingExtensions(t *testing.T) {
	t.Parallel()

	got, err := xwaylandArgs(DisplayParams{
		Display:       11,
		Geometry:      "1920x1080",
		AuthFile:      "/home/xorg-user/.Xserver",
		WindowManager: "awesome",
	})
	require.NoError(t, err)

	var disabled []string
	for i, arg := range got {
		require.NotEqual(t, "+extension", arg,
			"an extension is being enabled, which would let workloads observe each other: %v", got)

		if arg == "-extension" && i+1 < len(got) {
			disabled = append(disabled, got[i+1])
		}
	}

	require.Equal(t, []string{"MIT-SHM", "XTEST", "RECORD"}, disabled,
		"these extensions must stay disabled, see the comment in xwaylandArgs")
}

func TestWaitForSocket(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "qubesome")

	listening := make(chan net.Listener, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)

		lc := net.ListenConfig{}
		l, err := lc.Listen(context.Background(), "unix", path)
		if err != nil {
			// t.Logf is safe from a non-test goroutine, unlike
			// t.Fatal. Without this the test fails as a timeout and
			// points at waitForSocket rather than at the listener
			// that never started.
			t.Logf("failed to listen on %q: %v", path, err)
			close(listening)
			return
		}

		listening <- l
	}()

	err := waitForSocket(path, 5*time.Second)

	// Closing on the test goroutine, rather than through a t.Cleanup
	// registered by the goroutine above, which may run after this test
	// has returned and then never runs at all.
	if l := <-listening; l != nil {
		_ = l.Close()
	}

	require.NoError(t, err)
}

func TestWaitForSocketTimesOut(t *testing.T) {
	t.Parallel()

	err := waitForSocket(filepath.Join(t.TempDir(), "absent"), 100*time.Millisecond)
	require.Error(t, err)
}

func TestWaitForSocketRejectsNonSocket(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "qubesome")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	err := waitForSocket(path, time.Second)
	require.ErrorContains(t, err, "not a socket")
}

func TestCompositorEnvOverridesInherited(t *testing.T) {
	t.Parallel()

	got := compositorEnv([]string{"PATH=/bin", "XDG_RUNTIME_DIR=/run/user/1000"})

	require.Equal(t, "XDG_RUNTIME_DIR="+compositorRuntimeDir, got[len(got)-1],
		"the private runtime dir must win over an inherited one, os/exec takes the last duplicate")
	require.Contains(t, got, "PATH=/bin")
}

func TestXwaylandEnvPointsAtTheProfileCompositor(t *testing.T) {
	t.Parallel()

	got := xwaylandEnv([]string{"WAYLAND_DISPLAY=host-socket"})

	require.Equal(t, []string{
		"WAYLAND_DISPLAY=host-socket",
		"XDG_RUNTIME_DIR=" + compositorRuntimeDir,
		"WAYLAND_DISPLAY=" + compositorSocket,
	}, got)
}

func TestChildEnvNeverUsesTheSharedRuntimeDir(t *testing.T) {
	t.Parallel()

	for _, got := range [][]string{compositorEnv(nil), xwaylandEnv(nil)} {
		require.NotContains(t, got, "XDG_RUNTIME_DIR="+appRuntimeDir,
			"the compositor socket would be reachable by every workload")
	}
}

// Xwayland reports that its Wayland connection was reset and the window
// manager reports that the X server connection broke, neither of which
// says why. The compositor's own exit status does, so a failure names it.
func TestCompositorFailureReportsAnExitThatCameFirst(t *testing.T) {
	t.Parallel()

	exit := make(chan error, 1)
	exit <- errors.New("signal: segmentation fault")

	err := compositorFailure(exit)
	require.ErrorContains(t, err, "compositor exited first")
	require.ErrorContains(t, err, "signal: segmentation fault")

	require.Len(t, exit, 1, "the deferred reap still has to find a status")
}

// A compositor that exits cleanly before the window manager is still the
// cause of whatever the window manager then reported.
func TestCompositorFailureReportsACleanExitThatCameFirst(t *testing.T) {
	t.Parallel()

	exit := make(chan error, 1)
	exit <- nil

	require.ErrorContains(t, compositorFailure(exit), "compositor exited first")
	require.Len(t, exit, 1)
}

func TestCompositorFailureIsSilentWhileTheCompositorRuns(t *testing.T) {
	t.Parallel()

	require.NoError(t, compositorFailure(make(chan error, 1)))
}
