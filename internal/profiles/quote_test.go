package profiles

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "plain argument",
			args: []string{"--display"},
			want: "'--display'",
		},
		{
			name: "argument containing spaces",
			args: []string{"--wm", "exec dbus-run-session -- awesome"},
			want: "'--wm' 'exec dbus-run-session -- awesome'",
		},
		{
			name: "argument containing a single quote",
			args: []string{"it's"},
			want: `'it'\''s'`,
		},
		{
			name: "empty argument",
			args: []string{""},
			want: "''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := shellQuote(tt.args)
			require.Equal(t, tt.want, got)
		})
	}
}
