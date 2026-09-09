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

// Profile diagnoses one profile. cfg may be nil, which is itself a
// finding rather than an error, because a user whose config did not load
// is exactly who needs this command.
func Profile(env Env, cfg *types.Config, name string) []Check {
	configCheck := checkProfileConfig(cfg, name)
	if configCheck.Status != OK {
		// Every later check depends on a valid profile. Returning here
		// stops them from failing for the same reason and burying it.
		return []Check{configCheck}
	}

	profile := cfg.Profiles[name]
	src := resolveSource(env, cfg, name)

	// The path checks below read paths as a start would, which means
	// after the variables they are written against have been registered.
	primeExpansion(src, profile)

	sandboxCheck, running := checkProfileSandbox(env, name)

	return []Check{
		configCheck,
		checkProfileSource(src),
		checkProfileImage(env, profile.Image),
		sandboxCheck,
		checkProfileSocket(env, name, running),
		checkProfileCookies(env, name, running),
		checkMappedPaths(env, "profile paths", profile.Paths),
		checkProfileDevices(env, profile.HostAccess),
		checkExternalDrives(env, profile.ExternalDrives),
		checkProfileDisplay(env, profile.Display, running),
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

// checkProfileImage reports whether the profile's image is in the OCI
// store, which is where a sandbox takes its root filesystem from. Its
// absence is a Warn, not a Fail, since qubesome pulls a missing image on
// start.
func checkProfileImage(env Env, image string) Check {
	if !env.ImageInStore(image) {
		return Check{
			Name:   "profile image",
			Status: Warn,
			Detail: fmt.Sprintf("%s is not in the image store", image),
			Fix:    "It will be pulled on start, or pull it now with `qubesome images refresh`.",
		}
	}

	return Check{
		Name:   "profile image",
		Status: OK,
		Detail: fmt.Sprintf("%s is in the image store", image),
	}
}

// checkProfileSandbox reports whether the profile is running, and returns
// that so the later checks do not each read the state file again.
//
// A profile records its sandbox's pid and start time in a state file, and
// that file is the whole answer: it either describes a live process or it
// does not. There is no command that can fail to answer, so a profile
// that is not running is the only finding here, and it is a Warn because
// a profile that has not been started yet is not broken.
func checkProfileSandbox(env Env, name string) (Check, bool) {
	path := profiles.SandboxStatePath(name)

	if !env.SandboxAlive(path) {
		return Check{
			Name:   "profile sandbox",
			Status: Warn,
			Detail: "the profile is not running",
			Fix:    fmt.Sprintf("Start it with `qubesome start %s`.", name),
		}, false
	}

	return Check{
		Name:   "profile sandbox",
		Status: OK,
		Detail: fmt.Sprintf("%s records a running sandbox", path),
	}, true
}

// checkProfileSocket reports on the gRPC socket workloads use to reach
// the host.
func checkProfileSocket(env Env, name string, running bool) Check {
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
		if running {
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

	if !running {
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
func checkProfileCookies(env Env, name string, running bool) Check {
	if !running {
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

// checkExternalDrives reports on the drives the profile requires.
//
// It reads the kernel's mount table, the way the start does, rather than
// statting the mountpoint. A mountpoint is a directory that exists
// whether or not the drive is mounted over it, so statting it answers
// neither question: it passes for an unmounted drive whose directory was
// left behind, and fails for a mounted one whose directory the checking
// user cannot stat.
func checkExternalDrives(env Env, drives []string) Check {
	if len(drives) == 0 {
		return Check{
			Name:   "external drives",
			Status: OK,
			Detail: "no external drives are configured",
		}
	}

	var problems []string
	for _, d := range drives {
		label, device, mount, err := parseExternalDrive(d)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%q is not a valid entry: %s", d, err))
			continue
		}

		mounted, err := env.Mounted(device, mount)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s could not be checked: %s", label, err))
			continue
		}

		if !mounted {
			problems = append(problems, fmt.Sprintf("%s: %s is not mounted at %s", label, device, mount))
		}
	}

	if len(problems) > 0 {
		return Check{
			Name:   "external drives",
			Status: Fail,
			Detail: strings.Join(problems, ", "),
			Fix:    "The profile refuses to start without them. Mount them, or remove them from the config.",
		}
	}

	return Check{
		Name:   "external drives",
		Status: OK,
		Detail: fmt.Sprintf("all %d external drive(s) are mounted", len(drives)),
	}
}

// checkProfileDisplay reports on the profile's X server socket.
func checkProfileDisplay(env Env, display uint8, running bool) Check {
	path := fmt.Sprintf("/tmp/.X11-unix/X%d", display)

	_, err := env.Stat(path)
	present := err == nil

	switch {
	case present && running:
		return Check{
			Name:   "display",
			Status: OK,
			Detail: fmt.Sprintf("%s is present and the profile is up", path),
		}
	case present && !running:
		return Check{
			Name:   "display",
			Status: Warn,
			Detail: fmt.Sprintf("%s is present but the profile is not running, something else is using that display number", path),
			Fix:    "Change display in the profile config, since two profiles on one number collide.",
		}
	case !present && running:
		return Check{
			Name:   "display",
			Status: Fail,
			Detail: fmt.Sprintf("the profile is running but %s is not there", path),
			Fix:    "Check the profile's output to see why its X server did not start.",
		}
	default:
		return Check{
			Name:   "display",
			Status: OK,
			Detail: fmt.Sprintf("%s is not present, the profile is not running", path),
		}
	}
}
