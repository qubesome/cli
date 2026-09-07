package podman

import (
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRunUserParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		access    types.HostAccess
		wantArgs  []string
		wantPaths []string
	}{
		{
			name:   "isolated by default",
			access: types.HostAccess{},
			wantPaths: []string{
				"-v=/base/p/user/shm/work:/dev/shm:z",
				"-v=/base/p/user:/run/user/1000:z",
				"-v=/profile/p/machine-id:/etc/machine-id:ro,z",
			},
		},
		{
			name:   "host dbus keeps the host bus",
			access: types.HostAccess{Dbus: true},
			wantArgs: []string{
				"-v=/run/user/1000:/run/user/1000:z",
			},
			wantPaths: []string{
				"-v=/base/p/user/shm/work:/dev/shm:z",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args, paths := runUserParams(runUserInput{
				UserDir:    "/base/p/user",
				ProfileDir: "/profile/p",
				ShmDir:     "/base/p/user/shm/work",
				HostAccess: tc.access,
			})

			require.Equal(t, tc.wantArgs, args)
			require.Equal(t, tc.wantPaths, paths)
		})
	}
}

func TestRunUserParamsShmIsPerWorkload(t *testing.T) {
	t.Parallel()

	_, a := runUserParams(runUserInput{UserDir: "/u", ProfileDir: "/p", ShmDir: "/u/shm/alpha"})
	_, b := runUserParams(runUserInput{UserDir: "/u", ProfileDir: "/p", ShmDir: "/u/shm/beta"})

	require.NotEqual(t, a[0], b[0])
	require.Equal(t, "-v=/u/shm/alpha:/dev/shm:z", a[0])
	require.Equal(t, "-v=/u/shm/beta:/dev/shm:z", b[0])
}
