package drive

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMounts(t *testing.T) {
	tests := []struct {
		name         string
		drive        string
		mount        string
		want         bool
		readFileFunc func(name string) ([]byte, error)
		err          string
	}{
		{
			name:  "empty drive",
			mount: "/media",
			err:   "drive is empty",
		},
		{
			name:  "empty mount",
			drive: "/media",
			err:   "mount is empty",
		},
		{
			name:  "drive not found",
			drive: "foo-bar",
			mount: "/media",
			readFileFunc: func(name string) ([]byte, error) {
				return []byte("foo\nbar\n"), nil
			},
		},
		{
			name:  "mounts file not found",
			drive: "foo-bar",
			mount: "/media",
			readFileFunc: func(name string) ([]byte, error) {
				return nil, os.ErrNotExist
			},
			err: "failed to open mounts file: file does not exist",
		},
		{
			name:  "single mount point",
			drive: "foo-bar",
			mount: "/media/foo/bar",
			readFileFunc: func(name string) ([]byte, error) {
				return []byte("foo\nbar\nfoo-bar /media/foo/bar\n"), nil
			},
			want: true,
		},
		{
			name:  "multiple mount points",
			drive: "foo-bar",
			mount: "/media/foo/bar",
			readFileFunc: func(name string) ([]byte, error) {
				return []byte("foo-bar /foo/for/bar\nfoo\nbar\nfoo-bar /media/foo/bar\n"), nil
			},
			want: true,
		},
		{
			name:  "not right mount point",
			drive: "foo-bar",
			mount: "/media/bar/foo",
			readFileFunc: func(name string) ([]byte, error) {
				return []byte("foo\nbar\nfoo-bar /media/foo/bar\n"), nil
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			readFile = tc.readFileFunc
			got, err := Mounts(tc.drive, tc.mount)

			if tc.err == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}
