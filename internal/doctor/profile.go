package doctor

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/profiles"
	"github.com/qubesome/cli/internal/types"
)

// containerState is what checkProfileContainer found, passed to the
// later checks so that they do not each run ps again.
type containerState int

const (
	containerNotRunning containerState = iota
	containerUp
	containerExited
)

// Profile diagnoses one profile. cfg may be nil, which is itself a
// finding rather than an error, because a user whose config did not load
// is exactly who needs this command.
func Profile(env Env, cfg *types.Config, runner, name string) []Check {
	configCheck := checkProfileConfig(cfg, name)
	if configCheck.Status != OK {
		// Every later check depends on a valid profile. Returning here
		// stops them from failing for the same reason and burying it.
		return []Check{configCheck}
	}

	profile := cfg.Profiles[name]
	bin := files.ContainerRunnerBinary(runner)

	containerCheck, state := checkProfileContainer(env, bin, name)

	return []Check{
		configCheck,
		checkProfileImage(env, bin, profile.Image),
		containerCheck,
		checkProfileSocket(env, name, state),
		checkProfileCookies(env, name, state),
		checkProfilePaths(env, profile.Paths),
		checkExternalDrives(env, profile.ExternalDrives),
		checkProfileDisplay(env, profile.Display, state),
	}
}

// checkProfileConfig confirms that a config was loaded and that the
// requested profile exists and validates within it.
func checkProfileConfig(cfg *types.Config, name string) Check {
	if cfg == nil {
		return Check{
			Name:   "profile config",
			Status: Fail,
			Detail: "no qubesome config was loaded",
			Fix:    "Run qubesome from a directory with a qubesome config, or pass -git or -local to point at one.",
		}
	}

	profile, ok := cfg.Profiles[name]
	if !ok {
		names := make([]string, 0, len(cfg.Profiles))
		for n := range cfg.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)

		return Check{
			Name:   "profile config",
			Status: Fail,
			Detail: fmt.Sprintf("profile %q is not defined, known profiles: %s", name, strings.Join(names, ", ")),
			Fix:    "Check the profile name for typos, or add it to the config.",
		}
	}

	if err := profile.Validate(); err != nil {
		return Check{
			Name:   "profile config",
			Status: Fail,
			Detail: fmt.Sprintf("profile %q is invalid: %s", name, err),
			Fix:    "Fix the profile in the qubesome config so it validates.",
		}
	}

	return Check{
		Name:   "profile config",
		Status: OK,
		Detail: fmt.Sprintf("profile %q is defined and valid", name),
	}
}

// checkProfileImage reports whether the profile's image is present
// locally. Its absence is a Warn, not a Fail, since qubesome pulls a
// missing image on start.
func checkProfileImage(env Env, bin, image string) Check {
	if _, err := env.Output(bin, "image", "inspect", image); err != nil {
		return Check{
			Name:   "profile image",
			Status: Warn,
			Detail: fmt.Sprintf("%s is not present locally", image),
			Fix:    fmt.Sprintf("It will be pulled on start, or pull it now with `%s pull %s`.", bin, image),
		}
	}

	return Check{
		Name:   "profile image",
		Status: OK,
		Detail: fmt.Sprintf("%s is present locally", image),
	}
}

// containerName mirrors profiles.ContainerNameFormat from
// internal/profiles/profiles.go, which is safe to import directly here
// with no cycle, kept as its own helper only for the fmt.Sprintf call
// site.
func containerName(name string) string {
	return fmt.Sprintf(profiles.ContainerNameFormat, name)
}

// checkProfileContainer reports on the profile's container, and returns
// the state the later checks need so they do not run ps again.
func checkProfileContainer(env Env, bin, name string) (Check, containerState) {
	out, _ := env.Output(bin, "ps", "-a", "--filter", "name="+containerName(name),
		"--format", "{{.Names}} {{.Status}}")

	status := strings.TrimSpace(string(out))
	if status == "" {
		return Check{
			Name:   "profile container",
			Status: Warn,
			Detail: "the profile is not running",
			Fix:    fmt.Sprintf("Start it with `qubesome start %s`.", name),
		}, containerNotRunning
	}

	if strings.Contains(status, "Up") {
		return Check{
			Name:   "profile container",
			Status: OK,
			Detail: status,
		}, containerUp
	}

	return Check{
		Name:   "profile container",
		Status: Fail,
		Detail: status,
		Fix:    "The container runs with --rm, so its logs are already gone. Re-run with -i and start the display by hand to see the error.",
	}, containerExited
}

// checkProfileSocket reports on the gRPC socket workloads use to reach
// the host.
func checkProfileSocket(env Env, name string, state containerState) Check {
	path, err := files.SocketPath(name)
	if err != nil {
		return Check{
			Name:   "profile socket",
			Status: Fail,
			Detail: fmt.Sprintf("could not determine the socket path: %s", err),
		}
	}

	fi, statErr := env.Stat(path)
	if statErr != nil {
		if state == containerUp {
			return Check{
				Name:   "profile socket",
				Status: Fail,
				Detail: fmt.Sprintf("%s does not exist, workloads cannot reach the host", path),
				Fix:    "Restart the profile.",
			}
		}

		return Check{
			Name:   "profile socket",
			Status: OK,
			Detail: "nothing to serve, the profile is not running",
		}
	}

	if fi.Mode()&os.ModeSocket == 0 {
		return Check{
			Name:   "profile socket",
			Status: Fail,
			Detail: fmt.Sprintf("%s exists but is not a socket", path),
			Fix:    fmt.Sprintf("Remove %s.", path),
		}
	}

	if state != containerUp {
		return Check{
			Name:   "profile socket",
			Status: Fail,
			Detail: fmt.Sprintf("%s is left over from a profile that is gone", path),
			Fix:    fmt.Sprintf("Remove %s, since listening on an existing path fails and the next start would silently have nothing serving it.", path),
		}
	}

	return Check{
		Name:   "profile socket",
		Status: OK,
		Detail: fmt.Sprintf("%s is present and the profile is up", path),
	}
}

// checkProfileCookies reports on the Xauthority cookies workloads use to
// authenticate to the profile's X server.
func checkProfileCookies(env Env, name string, state containerState) Check {
	if state != containerUp {
		return Check{
			Name:   "profile cookies",
			Status: OK,
			Detail: "not running, cookies are created at start",
		}
	}

	serverPath, err := files.ServerCookiePath(name)
	if err != nil {
		return Check{
			Name:   "profile cookies",
			Status: Fail,
			Detail: fmt.Sprintf("could not determine the server cookie path: %s", err),
		}
	}

	clientPath, err := files.ClientCookiePath(name)
	if err != nil {
		return Check{
			Name:   "profile cookies",
			Status: Fail,
			Detail: fmt.Sprintf("could not determine the client cookie path: %s", err),
		}
	}

	var missing []string
	for _, p := range []string{serverPath, clientPath} {
		fi, err := env.Stat(p)
		if err != nil || fi.Size() == 0 {
			missing = append(missing, p)
		}
	}

	if len(missing) > 0 {
		return Check{
			Name:   "profile cookies",
			Status: Fail,
			Detail: fmt.Sprintf("missing or empty: %s", strings.Join(missing, ", ")),
			Fix:    "Restart the profile.",
		}
	}

	return Check{
		Name:   "profile cookies",
		Status: OK,
		Detail: "server and client cookies are present",
	}
}

// checkProfilePaths reports on the host source side of the profile's
// mapped paths.
func checkProfilePaths(env Env, paths []string) Check {
	if len(paths) == 0 {
		return Check{
			Name:   "profile paths",
			Status: OK,
			Detail: "no paths are configured",
		}
	}

	var missing []string
	for _, p := range paths {
		src := p
		if i := strings.Index(p, ":"); i >= 0 {
			src = p[:i]
		}

		if _, err := env.Stat(src); err != nil {
			missing = append(missing, src)
		}
	}

	if len(missing) > 0 {
		return Check{
			Name:   "profile paths",
			Status: Warn,
			Detail: fmt.Sprintf("missing on the host: %s", strings.Join(missing, ", ")),
			Fix:    "These are skipped with a warning at start. Create them, or remove them from the config.",
		}
	}

	return Check{
		Name:   "profile paths",
		Status: OK,
		Detail: fmt.Sprintf("all %d mapped paths are present", len(paths)),
	}
}

// checkExternalDrives reports on the mountpoints the profile requires.
func checkExternalDrives(env Env, drives []string) Check {
	if len(drives) == 0 {
		return Check{
			Name:   "external drives",
			Status: OK,
			Detail: "no external drives are configured",
		}
	}

	var missing []string
	for _, d := range drives {
		mount := d
		if i := strings.Index(d, ":"); i >= 0 {
			mount = d[i+1:]
		}

		if _, err := env.Stat(mount); err != nil {
			missing = append(missing, mount)
		}
	}

	if len(missing) > 0 {
		return Check{
			Name:   "external drives",
			Status: Fail,
			Detail: fmt.Sprintf("not mounted: %s", strings.Join(missing, ", ")),
			Fix:    "The profile refuses to start without them. Mount them, or remove them from the config.",
		}
	}

	return Check{
		Name:   "external drives",
		Status: OK,
		Detail: fmt.Sprintf("all %d external drives are mounted", len(drives)),
	}
}

// checkProfileDisplay reports on the profile's X server socket.
func checkProfileDisplay(env Env, display uint8, state containerState) Check {
	path := fmt.Sprintf("/tmp/.X11-unix/X%d", display)

	_, err := env.Stat(path)
	present := err == nil

	switch {
	case present && state == containerUp:
		return Check{
			Name:   "display",
			Status: OK,
			Detail: fmt.Sprintf("%s is present and the profile is up", path),
		}
	case present && state != containerUp:
		return Check{
			Name:   "display",
			Status: Warn,
			Detail: fmt.Sprintf("%s is present but the profile is not running, something else is using that display number", path),
			Fix:    "Change display in the profile config, since two profiles on one number collide.",
		}
	case !present && state == containerUp:
		return Check{
			Name:   "display",
			Status: Fail,
			Detail: fmt.Sprintf("the profile is running but %s is not there", path),
			Fix:    "Check the container's output to see why its X server did not start.",
		}
	default:
		return Check{
			Name:   "display",
			Status: OK,
			Detail: fmt.Sprintf("%s is not present, the profile is not running", path),
		}
	}
}
