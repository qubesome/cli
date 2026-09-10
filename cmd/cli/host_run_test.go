package cli

import (
	"testing"

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
