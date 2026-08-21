package dbusproxy

import (
	"testing"

	"github.com/qubesome/cli/internal/types"
)

func TestSessionProxyCommand(t *testing.T) {
	t.Parallel()

	got := proxyShellCommand(
		"unix:path=/run/host-dbus/session",
		"/run/user/1000/qube/foo-work/session",
		"/run/user/1000/qube/foo-work/session.pid",
		types.ProxyFlags([]string{"talk=org.freedesktop.Notifications"}),
	)
	want := "mkdir -p /run/user/1000/qube/foo-work && " +
		"xdg-dbus-proxy unix:path=/run/host-dbus/session /run/user/1000/qube/foo-work/session " +
		"--filter --talk=org.freedesktop.Notifications & " +
		"echo $! > /run/user/1000/qube/foo-work/session.pid; wait"
	if got != want {
		t.Errorf("\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStartArgs(t *testing.T) {
	t.Parallel()

	spec := Spec{
		Runner:           "podman",
		ProfileContainer: "qubesome-work",
		WorkloadName:     "foo-work",
		Session:          []string{"talk=org.freedesktop.Notifications"},
		System:           nil,
	}

	sockets, execArgs := spec.startArgs()

	if sockets.Session != "/run/user/1000/qube/foo-work/session" {
		t.Errorf("session socket: got %q", sockets.Session)
	}
	if sockets.System != "" {
		t.Errorf("system socket: expected empty, got %q", sockets.System)
	}
	// One exec invocation for the session bus.
	if len(execArgs) != 1 {
		t.Fatalf("expected 1 exec invocation, got %d", len(execArgs))
	}
	got := execArgs[0]
	if got[0] != "exec" || got[1] != "-d" || got[2] != "qubesome-work" {
		t.Errorf("exec prefix wrong: %v", got[:3])
	}
	if got[3] != "sh" || got[4] != "-c" {
		t.Errorf("expected sh -c, got %v", got[3:5])
	}
}

func TestHostSocket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		hostRunUser string
		container   string
		want        string
	}{
		{
			name:        "session socket maps to host path",
			hostRunUser: "/home/u/.config/qubesome/profiles/work/run-user",
			container:   "/run/user/1000/qube/foo-work/session",
			want:        "/home/u/.config/qubesome/profiles/work/run-user/qube/foo-work/session",
		},
		{
			name:        "empty socket returns empty",
			hostRunUser: "/home/u/run-user",
			container:   "",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := HostSocket(tt.hostRunUser, tt.container); got != tt.want {
				t.Errorf("HostSocket(%q, %q) = %q want %q", tt.hostRunUser, tt.container, got, tt.want)
			}
		})
	}
}
