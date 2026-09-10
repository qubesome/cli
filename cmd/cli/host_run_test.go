package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostRunEnv(t *testing.T) {
	t.Parallel()

	got := hostRunEnv([]string{
		"HOME=/home/user",
		"PATH=/usr/bin",
		"DISPLAY=:0",
		"XAUTHORITY=/home/user/.Xauthority",
	}, 21, "/home/user/.qubesome/run/work/.Xclient-cookie")

	require.Equal(t, []string{
		"HOME=/home/user",
		"PATH=/usr/bin",
		"DISPLAY=:0",
		"XAUTHORITY=/home/user/.Xauthority",
		"DISPLAY=:21",
		"XAUTHORITY=/home/user/.qubesome/run/work/.Xclient-cookie",
	}, got)
}

func TestHostRunEnvDropsWaylandDisplay(t *testing.T) {
	t.Parallel()

	got := hostRunEnv([]string{
		"HOME=/home/user",
		"WAYLAND_DISPLAY=wayland-0",
	}, 21, "/cookie")

	require.NotContains(t, got, "WAYLAND_DISPLAY=wayland-0")
	require.Contains(t, got, "HOME=/home/user")
}

// A window manager that spawns through startup notification records the
// workspace it spawned from against the id it exports. Carrying that id
// into another display server hands the profile's window manager a
// sequence it never started, and the window lands wherever that resolves
// to instead of where the user is looking.
func TestHostRunEnvDropsTheLaunchingSession(t *testing.T) {
	t.Parallel()

	got := hostRunEnv([]string{
		"HOME=/home/user",
		"PATH=/usr/bin",
		"DESKTOP_STARTUP_ID=host/awesome/1-2-3_TIME12345",
		"XDG_ACTIVATION_TOKEN=abcdef",
		"WAYLAND_DISPLAY=wayland-0",
	}, 21, "/cookie")

	for _, unwanted := range []string{
		"DESKTOP_STARTUP_ID",
		"XDG_ACTIVATION_TOKEN",
		"WAYLAND_DISPLAY",
	} {
		for _, e := range got {
			assert.NotContains(t, e, unwanted+"=",
				"%s names the session the command was launched from", unwanted)
		}
	}

	assert.Contains(t, got, "HOME=/home/user", "the host environment is otherwise kept")
	assert.Contains(t, got, "PATH=/usr/bin")
}
