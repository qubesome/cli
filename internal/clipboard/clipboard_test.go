package clipboard

import (
	"testing"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestCopy(t *testing.T) {
	tests := []struct {
		name    string
		from    *types.Profile
		to      *types.Profile
		target  string
		wantErr string
	}{
		{
			name:    "same display",
			from:    &types.Profile{Display: 1},
			to:      &types.Profile{Display: 1},
			target:  "",
			wantErr: "cannot copy clipboard within the same display",
		},
		{
			name:    "invalid type",
			from:    &types.Profile{Display: 0},
			to:      &types.Profile{Display: 1},
			target:  "foo",
			wantErr: "unsupported copy type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var opts []command.Option[Options]
			opts = append(opts, WithSourceProfile(tc.from))
			opts = append(opts, WithTargetProfile(tc.to))
			opts = append(opts, WithContentType(tc.target))

			err := Run(opts...)

			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}
