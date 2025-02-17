package flatpak_test

import (
	"testing"

	"github.com/qubesome/cli/internal/flatpak"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    flatpak.Options
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty flatpak",
			opts:    flatpak.Options{},
			wantErr: true,
			errMsg:  "missing flatpak name",
		},
		{
			name: "flatpak name: ../",
			opts: flatpak.Options{
				Name: "../",
			},
			wantErr: true,
			errMsg:  "invalid flatpak name",
		},
		{
			name: "flatpak name: foo..ball",
			opts: flatpak.Options{
				Name: "foo..ball",
			},
			wantErr: true,
			errMsg:  "invalid flatpak name",
		},
		{
			name: "flatpak name: foo../ball",
			opts: flatpak.Options{
				Name: "foo../ball",
			},
			wantErr: true,
			errMsg:  "invalid flatpak name",
		},
		{
			name: "flatpak name: foo.bar.ball",
			opts: flatpak.Options{
				Name:   "foo.bar.ball",
				Config: &types.Config{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.opts.Validate()

			if tc.wantErr {
				assert.ErrorContains(t, err, tc.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
