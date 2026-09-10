package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The gateway belongs to the session and not to a profile, so its status
// has no profile to be told which config to read. Every active profile
// naming the same file is the ordinary case, since one qubesome config
// usually defines several profiles, and that file is the answer. Two
// profiles started from different files is the case that has no answer.
func TestSessionConfigPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		active  []string
		want    string
		wantOne bool
	}{
		{
			name:   "nothing active",
			active: nil,
		},
		{
			name:    "one profile",
			active:  []string{"/home/u/dotfiles/qubesome.yaml"},
			want:    "/home/u/dotfiles/qubesome.yaml",
			wantOne: true,
		},
		{
			name: "several profiles from one config",
			active: []string{
				"/home/u/dotfiles/qubesome.yaml",
				"/home/u/dotfiles/qubesome.yaml",
			},
			want:    "/home/u/dotfiles/qubesome.yaml",
			wantOne: true,
		},
		{
			name: "profiles from different configs",
			active: []string{
				"/home/u/dotfiles/qubesome.yaml",
				"/home/u/work/qubesome.yaml",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := sessionConfigPath(tc.active)
			assert.Equal(t, tc.wantOne, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
