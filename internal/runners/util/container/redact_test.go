package container

import (
	"reflect"
	"testing"
)

func TestRedactEnvArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "short flag with separate value",
			args: []string{"run", "-e", "TOKEN=secret", "image"},
			want: []string{"run", "-e", "TOKEN=REDACTED", "image"},
		},
		{
			name: "long flag with separate value",
			args: []string{"run", "--env", "PASSWORD=two=parts", "image"},
			want: []string{"run", "--env", "PASSWORD=REDACTED", "image"},
		},
		{
			name: "short flag with inline value",
			args: []string{"run", "-e=API_KEY=secret", "image"},
			want: []string{"run", "-e=API_KEY=REDACTED", "image"},
		},
		{
			name: "long flag with inline value",
			args: []string{"run", "--env=CLIENT_SECRET=secret", "image"},
			want: []string{"run", "--env=CLIENT_SECRET=REDACTED", "image"},
		},
		{
			name: "inherited environment variable",
			args: []string{"run", "-e", "TOKEN", "--env=PASSWORD", "image"},
			want: []string{"run", "-e", "TOKEN", "--env=PASSWORD", "image"},
		},
		{
			name: "unrelated arguments",
			args: []string{"run", "--entrypoint=/bin/env", "image", "TOKEN=secret"},
			want: []string{"run", "--entrypoint=/bin/env", "image", "TOKEN=secret"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			original := append([]string(nil), tc.args...)
			got := RedactEnvArgs(tc.args)

			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("RedactEnvArgs() = %q, want %q", got, tc.want)
			}
			if !reflect.DeepEqual(tc.args, original) {
				t.Fatalf("RedactEnvArgs() mutated input: got %q, want %q", tc.args, original)
			}
		})
	}
}
