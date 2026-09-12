package tz

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZoneOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// zoneFile is a regular file created under the root. Empty
		// creates none, which leaves a link dangling.
		zoneFile string
		// link is what etc/localtime points at. Empty makes it a
		// regular file instead.
		link string
		// wantHost is relative to the root, and empty means the host
		// has no timezone to give.
		wantHost    string
		wantSandbox string
		wantTZ      string
	}{
		{
			name:        "a named zone",
			zoneFile:    "usr/share/zoneinfo/Europe/London",
			link:        "../usr/share/zoneinfo/Europe/London",
			wantHost:    "usr/share/zoneinfo/Europe/London",
			wantSandbox: "/usr/share/zoneinfo/Europe/London",
			wantTZ:      "Europe/London",
		},
		{
			name:        "a name of more than two components",
			zoneFile:    "usr/share/zoneinfo/America/Argentina/Buenos_Aires",
			link:        "../usr/share/zoneinfo/America/Argentina/Buenos_Aires",
			wantHost:    "usr/share/zoneinfo/America/Argentina/Buenos_Aires",
			wantSandbox: "/usr/share/zoneinfo/America/Argentina/Buenos_Aires",
			wantTZ:      "America/Argentina/Buenos_Aires",
		},
		{
			name:        "a name of one component",
			zoneFile:    "usr/share/zoneinfo/UTC",
			link:        "../usr/share/zoneinfo/UTC",
			wantHost:    "usr/share/zoneinfo/UTC",
			wantSandbox: "/usr/share/zoneinfo/UTC",
			wantTZ:      "UTC",
		},
		{
			name:        "a database the host does not keep under /usr/share",
			zoneFile:    "etc/zoneinfo/Europe/London",
			link:        "zoneinfo/Europe/London",
			wantHost:    "etc/zoneinfo/Europe/London",
			wantSandbox: "/usr/share/zoneinfo/Europe/London",
			wantTZ:      "Europe/London",
		},
		{
			name:        "a zone file copied into place, which names nothing",
			wantHost:    "etc/localtime",
			wantSandbox: "/usr/share/zoneinfo/localtime",
			wantTZ:      ":/usr/share/zoneinfo/localtime",
		},
		{
			name:        "a name no timezone database would have written",
			zoneFile:    "usr/share/zoneinfo/Europe/Lon don",
			link:        "../usr/share/zoneinfo/Europe/Lon don",
			wantHost:    "usr/share/zoneinfo/Europe/Lon don",
			wantSandbox: "/usr/share/zoneinfo/localtime",
			wantTZ:      ":/usr/share/zoneinfo/localtime",
		},
		{
			name: "a link to a zone that is not there",
			link: "../usr/share/zoneinfo/Europe/London",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if tc.zoneFile != "" {
				writeFile(t, filepath.Join(root, tc.zoneFile))
			}

			localtime := filepath.Join(root, "etc", "localtime")
			require.NoError(t, os.MkdirAll(filepath.Dir(localtime), 0o755))
			if tc.link == "" {
				writeFile(t, localtime)
			} else {
				require.NoError(t, os.Symlink(tc.link, localtime))
			}

			want := Zone{SandboxPath: tc.wantSandbox, TZ: tc.wantTZ}
			if tc.wantHost != "" {
				want.HostPath = resolved(t, filepath.Join(root, tc.wantHost))
			}

			assert.Equal(t, want, zoneOf(localtime))
		})
	}
}

// A host that names a zone by one of the database's own aliases still
// shares a file every image has under the name it has there.
func TestZoneOfAnAlias(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	london := filepath.Join(root, "usr/share/zoneinfo/Europe/London")
	writeFile(t, london)
	require.NoError(t, os.Symlink("Europe/London", filepath.Join(root, "usr/share/zoneinfo/GB")))

	localtime := filepath.Join(root, "etc", "localtime")
	require.NoError(t, os.MkdirAll(filepath.Dir(localtime), 0o755))
	require.NoError(t, os.Symlink("../usr/share/zoneinfo/GB", localtime))

	assert.Equal(t, Zone{
		HostPath:    resolved(t, london),
		SandboxPath: "/usr/share/zoneinfo/Europe/London",
		TZ:          "Europe/London",
	}, zoneOf(localtime))
}

func TestZoneOfAHostWithoutOne(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Zone{}, zoneOf(filepath.Join(t.TempDir(), "etc", "localtime")))
}

func writeFile(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("TZif"), 0o600))
}

// resolved is what the zone file's path is once the temporary directory's
// own symlinks are gone, which is what zoneOf reports.
func resolved(t *testing.T, path string) string {
	t.Helper()

	out, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	return out
}
