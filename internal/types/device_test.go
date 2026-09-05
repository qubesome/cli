package types

import "testing"

func TestParseDevice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		device    string
		wantSrc   string
		wantDst   string
		wantPerms string
		wantErr   bool
	}{
		{device: "/dev/net/tun", wantSrc: "/dev/net/tun", wantDst: "/dev/net/tun", wantPerms: "rwm"},
		{device: "/dev/dri:/dev/dri", wantSrc: "/dev/dri", wantDst: "/dev/dri", wantPerms: "rwm"},
		{device: "/dev/null:/dev/sda:rw", wantSrc: "/dev/null", wantDst: "/dev/sda", wantPerms: "rw"},
		{device: "/dev/null:/dev/null:m", wantSrc: "/dev/null", wantDst: "/dev/null", wantPerms: "m"},
		{device: "", wantErr: true},
		{device: "/etc/shadow", wantErr: true},
		{device: "/dev/null:/etc/shadow", wantErr: true},
		{device: "/dev/../etc/shadow", wantErr: true},
		{device: "/dev/null:/dev/sda:rwx", wantErr: true},
		{device: "/dev/null:/dev/sda:rrr", wantErr: true},
		{device: "/dev/null:/dev/sda:rr", wantErr: true},
		{device: "/dev/null:/dev/sda:wmw", wantErr: true},
		{device: "/dev/null:/dev/sda:mwr", wantSrc: "/dev/null", wantDst: "/dev/sda", wantPerms: "mwr"},
		{device: "/dev/null:/dev/sda:rwm:extra", wantErr: true},
		{device: "/dev", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.device, func(t *testing.T) {
			t.Parallel()

			src, dst, perms, err := ParseDevice(tc.device)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.device)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src != tc.wantSrc || dst != tc.wantDst || perms != tc.wantPerms {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)",
					src, dst, perms, tc.wantSrc, tc.wantDst, tc.wantPerms)
			}
		})
	}
}

func TestApplyProfileDevices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		profile  []string
		workload []string
		want     []string
	}{
		{
			name:     "exact match is allowed",
			profile:  []string{"/dev/net/tun"},
			workload: []string{"/dev/net/tun"},
			want:     []string{"/dev/net/tun"},
		},
		{
			name:     "descendant of allowed dir",
			profile:  []string{"/dev/bus/usb"},
			workload: []string{"/dev/bus/usb/001/002"},
			want:     []string{"/dev/bus/usb/001/002"},
		},
		{
			name:     "source outside the allowlist is dropped",
			profile:  []string{"/dev/net/tun"},
			workload: []string{"/dev/sda"},
			want:     []string{},
		},
		{
			name:     "allowed source remapped onto another device is matched on source",
			profile:  []string{"/dev/null"},
			workload: []string{"/dev/null:/dev/sda:rwm"},
			want:     []string{"/dev/null:/dev/sda:rwm"},
		},
		{
			name:     "disallowed source hidden behind an allowed destination",
			profile:  []string{"/dev/null"},
			workload: []string{"/dev/sda:/dev/null:rwm"},
			want:     []string{},
		},
		{
			name:     "malformed device is dropped",
			profile:  []string{"/dev/null"},
			workload: []string{"/dev/null:/etc/shadow"},
			want:     []string{},
		},
		{
			name:     "no profile devices drops everything",
			profile:  nil,
			workload: []string{"/dev/net/tun"},
			want:     []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := Workload{Name: "w", HostAccess: HostAccess{Devices: tc.workload}}
			p := &Profile{Name: "p", HostAccess: HostAccess{Devices: tc.profile}}

			got := w.ApplyProfile(p).Workload.HostAccess.Devices
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
