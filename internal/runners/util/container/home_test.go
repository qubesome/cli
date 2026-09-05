package container

import "testing"

func TestHomeDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     imageConfig
		want    string
		wantErr bool
	}{
		{name: "HOME wins", cfg: imageConfig{User: "chrome", Env: []string{"PATH=/bin", "HOME=/var/lib/chrome"}}, want: "/var/lib/chrome"},
		{name: "named user", cfg: imageConfig{User: "chrome"}, want: "/home/chrome"},
		{name: "user and group", cfg: imageConfig{User: "chrome:chrome"}, want: "/home/chrome"},
		{name: "no user", cfg: imageConfig{}, want: "/root"},
		{name: "root", cfg: imageConfig{User: "root"}, want: "/root"},
		{name: "uid zero", cfg: imageConfig{User: "0"}, want: "/root"},
		{name: "numeric uid", cfg: imageConfig{User: "1000"}, wantErr: true},
		{name: "relative HOME", cfg: imageConfig{Env: []string{"HOME=home/chrome"}}, wantErr: true},
		{name: "unclean HOME", cfg: imageConfig{Env: []string{"HOME=/home/../etc"}}, wantErr: true},
		{name: "HOME with a colon", cfg: imageConfig{Env: []string{"HOME=/home/ch:rome"}}, wantErr: true},
		{name: "user with a colon in the name", cfg: imageConfig{User: "ch:rome:g"}, wantErr: true},
		{name: "empty HOME falls back", cfg: imageConfig{User: "chrome", Env: []string{"HOME="}}, want: "/home/chrome"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := homeDir(tc.cfg, "org/image:tag")
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
