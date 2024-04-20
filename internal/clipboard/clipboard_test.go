package clipboard

import (
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestCopy(t *testing.T) {
	tests := []struct {
		name    string
		from    uint8
		to      types.Profile
		target  string
		wantErr string
	}{
		{
			name:    "same display",
			from:    1,
			to:      types.Profile{Display: 1},
			target:  "",
			wantErr: "cannot copy clipboard within the same display",
		},
		{
			name:    "invalid type",
			from:    0,
			to:      types.Profile{Display: 1},
			target:  "foo",
			wantErr: "unsupported copy type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Copy(tc.from, tc.to, tc.target)

			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}
