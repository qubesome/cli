package profiles

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompositorArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   displayParams
		want []string
	}{
		{
			name: "x11 host",
			in: displayParams{
				Geometry:      "1920x1080",
				WaylandSocket: "qubesome",
			},
			want: []string{
				"--backend=x11",
				"--width=1920",
				"--height=1080",
				"--shell=kiosk-shell.so",
				"--socket=qubesome",
			},
		},
		{
			name: "wayland host",
			in: displayParams{
				Geometry:      "3440x1440",
				WaylandSocket: "qubesome",
				HostWayland:   true,
			},
			want: []string{
				"--backend=wayland",
				"--width=3440",
				"--height=1440",
				"--shell=kiosk-shell.so",
				"--socket=qubesome",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := compositorArgs(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestCompositorArgsBadGeometry(t *testing.T) {
	t.Parallel()

	for _, g := range []string{"", "1920", "1920x", "x1080", "axb", "1920x1080x1"} {
		_, err := compositorArgs(displayParams{Geometry: g, WaylandSocket: "qubesome"})
		require.Error(t, err, "geometry %q should be rejected", g)
	}
}
