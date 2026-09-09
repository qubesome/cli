package container

import (
	"testing"

	"github.com/qubesome/cli/internal/images"
)

func TestHomeDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		bundle  images.Bundle
		want    string
		wantErr bool
	}{
		{name: "HOME wins", bundle: images.Bundle{UID: 1000, Env: []string{"PATH=/bin", "HOME=/var/lib/chrome"}}, want: "/var/lib/chrome"},
		{name: "no uid, no HOME falls back to root", bundle: images.Bundle{}, want: "/root"},
		{name: "uid zero, no HOME falls back to root", bundle: images.Bundle{UID: 0}, want: "/root"},
		{name: "nonzero uid with no HOME has no name to build a home from", bundle: images.Bundle{UID: 1000}, wantErr: true},
		{name: "relative HOME", bundle: images.Bundle{Env: []string{"HOME=home/chrome"}}, wantErr: true},
		{name: "unclean HOME", bundle: images.Bundle{Env: []string{"HOME=/home/../etc"}}, wantErr: true},
		{name: "HOME with a colon", bundle: images.Bundle{Env: []string{"HOME=/home/ch:rome"}}, wantErr: true},
		{
			name:   "duplicate HOME takes the last",
			bundle: images.Bundle{UID: 1000, Env: []string{"HOME=/home/first", "PATH=/bin", "HOME=/home/last"}},
			want:   "/home/last",
		},
		{
			name:   "duplicate HOME where the last is empty falls back to root uid",
			bundle: images.Bundle{UID: 0, Env: []string{"HOME=/home/first", "HOME="}},
			want:   "/root",
		},
		{
			name:    "duplicate HOME where the last is empty falls back and fails for a nonzero uid",
			bundle:  images.Bundle{UID: 1000, Env: []string{"HOME=/home/first", "HOME="}},
			wantErr: true,
		},
		{name: "empty HOME falls back to root uid", bundle: images.Bundle{UID: 0, Env: []string{"HOME="}}, want: "/root"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := homeDir(tc.bundle)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
