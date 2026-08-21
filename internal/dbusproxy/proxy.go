// Package dbusproxy starts xdg-dbus-proxy instances inside a profile's
// container so workloads reach only a filtered view of the host dbus.
package dbusproxy

import (
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/execabs"

	"github.com/qubesome/cli/internal/types"
)

// Container-side paths. The profile container mounts the raw host buses at
// these locations (see profiles.go) and the shared run-user dir at
// /run/user/1000.
const (
	hostSessionAddr = "unix:path=/run/host-dbus/session"
	hostSystemAddr  = "unix:path=/run/host-dbus/system"
	sharedRunUser   = "/run/user/1000"
)

// Sockets holds the container-side filtered socket paths (empty when that bus
// has no filtered access).
type Sockets struct {
	Session string
	System  string
}

// Spec describes one effective workload's filtered proxies.
type Spec struct {
	Runner           string
	ProfileContainer string
	WorkloadName     string
	Session          []string
	System           []string
}

func (s Spec) dir() string {
	return path.Join(sharedRunUser, "qube", s.WorkloadName)
}

// HostSocket maps a container-side socket path returned by Start to its path on
// the host, given the host directory that is bind-mounted at /run/user/1000 in
// the profile container. It returns an empty string for an empty socket.
func HostSocket(hostRunUser, containerSocket string) string {
	if containerSocket == "" {
		return ""
	}
	rel := strings.TrimPrefix(containerSocket, sharedRunUser+"/")
	return filepath.Join(hostRunUser, rel)
}

// proxyShellCommand builds the shell command run (via runner exec -d) inside
// the profile container. It backgrounds the proxy, records its pid for
// teardown, then waits so the detached exec stays alive with the proxy.
func proxyShellCommand(busAddr, socketPath, pidPath string, flags []string) string {
	dir := path.Dir(socketPath)
	proxy := strings.Join(append([]string{"xdg-dbus-proxy", busAddr, socketPath}, flags...), " ")
	return fmt.Sprintf("mkdir -p %s && %s & echo $! > %s; wait", dir, proxy, pidPath)
}

// startArgs returns the filtered socket paths and the runner exec argument
// lists to launch each proxy. Buses with no rules are skipped.
func (s Spec) startArgs() (Sockets, [][]string) {
	var sockets Sockets
	var invocations [][]string

	add := func(busAddr, name string, rules []string) string {
		sock := path.Join(s.dir(), name)
		pid := path.Join(s.dir(), name+".pid")
		cmd := proxyShellCommand(busAddr, sock, pid, types.ProxyFlags(rules))
		invocations = append(invocations, []string{"exec", "-d", s.ProfileContainer, "sh", "-c", cmd})
		return sock
	}

	// A filtered bus with an empty rule list still gets a proxy: the workload
	// asked for the bus, and --filter denies everything, which is the correct
	// fail-closed behaviour. Only skip a bus the workload never referenced.
	if s.Session != nil {
		sockets.Session = add(hostSessionAddr, "session", s.Session)
	}
	if s.System != nil {
		sockets.System = add(hostSystemAddr, "system", s.System)
	}
	return sockets, invocations
}

// Start launches the proxies inside the profile container and waits for their
// sockets to appear, returning the container-side socket paths.
//
// The proxies are not tied to the caller's lifetime. The workload container
// runs detached, so the qubesome process exits shortly after launching it.
// The proxies run inside the long-lived profile container and are reaped when
// that container stops. Start first cleans up any proxy left by a previous run
// of the same workload so a relaunch does not fail to bind an existing socket.
func (s Spec) Start() (Sockets, error) {
	sockets, invocations := s.startArgs()
	if len(invocations) == 0 {
		return sockets, nil
	}

	s.cleanup()

	for _, args := range invocations {
		slog.Debug(s.Runner + " " + strings.Join(args, " "))
		if err := execabs.Command(s.Runner, args...).Run(); err != nil {
			s.cleanup()
			return Sockets{}, fmt.Errorf("failed to start dbus proxy: %w", err)
		}
	}

	if err := s.waitForSockets(sockets); err != nil {
		s.cleanup()
		return Sockets{}, err
	}
	return sockets, nil
}

func (s Spec) waitForSockets(sockets Sockets) error {
	deadline := time.Now().Add(5 * time.Second)
	check := func(sock string) error {
		if sock == "" {
			return nil
		}
		for time.Now().Before(deadline) {
			args := []string{"exec", s.ProfileContainer, "test", "-S", sock}
			if err := execabs.Command(s.Runner, args...).Run(); err == nil {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("dbus proxy socket %q not ready", sock)
	}
	if err := check(sockets.Session); err != nil {
		return err
	}
	return check(sockets.System)
}

// cleanup kills any proxies previously started for this workload and removes
// their sockets and pid files. It is safe to call when nothing is running.
func (s Spec) cleanup() {
	kill := path.Join(s.dir(), "*.pid")
	cmd := fmt.Sprintf("for p in %s; do kill \"$(cat \"$p\")\" 2>/dev/null; done; rm -rf %s", kill, s.dir())
	args := []string{"exec", s.ProfileContainer, "sh", "-c", cmd}
	if err := execabs.Command(s.Runner, args...).Run(); err != nil {
		slog.Warn("failed to clean up dbus proxy", "workload", s.WorkloadName, "err", err)
	}
}
