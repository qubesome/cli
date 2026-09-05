package types

import "testing"

func TestWorkloadValidateArgs(t *testing.T) {
	t.Parallel()

	base := func() Workload {
		return Workload{Name: "w", Image: "org/image:tag", Command: "cmd"}
	}

	tests := []struct {
		name    string
		mutate  func(*Workload)
		wantErr bool
	}{
		{name: "no args", mutate: func(*Workload) {}},
		{name: "valid x11Args", mutate: func(w *Workload) { w.X11Args = []string{"--x11"} }},
		{name: "empty x11Args entry", mutate: func(w *Workload) { w.X11Args = []string{""} }, wantErr: true},
		{name: "empty waylandArgs entry", mutate: func(w *Workload) { w.WaylandArgs = []string{""} }, wantErr: true},
		{name: "empty noGpuArgs entry", mutate: func(w *Workload) { w.NoGPUArgs = []string{""} }, wantErr: true},
		{
			name:    "over-long x11Args entry",
			mutate:  func(w *Workload) { w.X11Args = []string{string(make([]byte, 251))} },
			wantErr: true,
		},
		{
			name:   "valid device",
			mutate: func(w *Workload) { w.HostAccess.Devices = []string{"/dev/net/tun"} },
		},
		{
			name:    "device outside /dev",
			mutate:  func(w *Workload) { w.HostAccess.Devices = []string{"/etc/shadow"} },
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := base()
			tc.mutate(&w)

			err := w.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProfileValidateFlatpaksAndDevices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile Profile
		wantErr bool
	}{
		{
			name:    "valid flatpak",
			profile: Profile{Name: "p", WindowManager: "wm", Flatpaks: []string{"org.kde.francis"}},
		},
		{
			name:    "flatpak with path traversal",
			profile: Profile{Name: "p", WindowManager: "wm", Flatpaks: []string{"../../etc/passwd"}},
			wantErr: true,
		},
		{
			name:    "flatpak with quote",
			profile: Profile{Name: "p", WindowManager: "wm", Flatpaks: []string{"org.kde'.francis"}},
			wantErr: true,
		},
		{
			name:    "valid device",
			profile: Profile{Name: "p", WindowManager: "wm", HostAccess: HostAccess{Devices: []string{"/dev/net/tun"}}},
		},
		{
			name:    "device outside /dev",
			profile: Profile{Name: "p", WindowManager: "wm", HostAccess: HostAccess{Devices: []string{"/etc"}}},
			wantErr: true,
		},
		{
			name:    "device escaping /dev",
			profile: Profile{Name: "p", WindowManager: "wm", HostAccess: HostAccess{Devices: []string{"/dev/../etc"}}},
			wantErr: true,
		},
		{
			name:    "device with a trailing slash",
			profile: Profile{Name: "p", WindowManager: "wm", HostAccess: HostAccess{Devices: []string{"/dev/net/"}}},
			wantErr: true,
		},
		{
			name:    "device mapping is not a grant",
			profile: Profile{Name: "p", WindowManager: "wm", HostAccess: HostAccess{Devices: []string{"/dev/null:/dev/sda:rwm"}}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.profile.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestWorkloadValidateArgFieldOrder(t *testing.T) {
	t.Parallel()

	w := Workload{
		Name:        "w",
		Image:       "org/image:tag",
		Command:     "cmd",
		Args:        []string{""},
		X11Args:     []string{""},
		WaylandArgs: []string{""},
		NoGPUArgs:   []string{""},
	}

	// Every field is invalid, so the reported one must not depend on map
	// iteration order.
	for range 20 {
		err := w.Validate()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got := err.Error(); got != "args cannot be empty" {
			t.Fatalf("got %q, want %q", got, "args cannot be empty")
		}
	}
}
