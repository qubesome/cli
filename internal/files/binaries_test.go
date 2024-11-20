package files_test

import (
	"testing"

	"github.com/qubesome/cli/internal/files"
	"github.com/stretchr/testify/assert"
)

func TestContainerRunnerBinary(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{
			in:   "",
			want: "/usr/bin/podman",
		},
		{
			in:   "podman",
			want: "/usr/bin/podman",
		},
		{
			in:   "docker",
			want: "/usr/bin/docker",
		},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := files.ContainerRunnerBinary(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}
