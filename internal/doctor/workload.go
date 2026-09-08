package doctor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"go.yaml.in/yaml/v3"
)

// Workload diagnoses one workload of a profile.
func Workload(env Env, cfg *types.Config, runner, profileName, workloadName string) []Check {
	src := resolveSource(env, cfg, profileName)

	configCheck := checkWorkloadConfig(cfg, src, profileName, workloadName)
	if configCheck.Status != OK {
		// Every later check depends on a valid, locatable workload.
		// Returning here stops them from failing for the same reason
		// and burying it.
		return []Check{configCheck}
	}

	profile := cfg.Profiles[profileName]
	bin := files.ContainerRunnerBinary(runnerFor(runner, profile))

	// ApplyProfile matches a workload's paths against the profile's
	// allowlist with both sides expanded, and the path check below reads
	// them as a start would, so the variables they are written against
	// have to be registered before either runs.
	primeExpansion(src, profile)

	w, err := loadWorkload(src, profile, workloadName)
	if err != nil {
		return []Check{
			configCheck,
			{
				Name:   "workload image",
				Status: Fail,
				Detail: fmt.Sprintf("could not read the workload file: %s", err),
			},
		}
	}

	effective := w.ApplyProfile(&profile)

	return []Check{
		configCheck,
		checkWorkloadImage(env, bin, w.Image),
		checkWorkloadProfileRunning(env, bin, profileName),
		checkWorkloadHostAccess(w, effective),
		checkWorkloadDevices(env, effective.Workload.HostAccess),
		checkMappedPaths(env, "workload paths", effective.Workload.HostAccess.Paths),
		checkWorkloadValidation(effective),
	}
}

// checkWorkloadConfig confirms that a config was loaded, that the
// requested profile exists, and that the requested workload can be found
// among the profile's workload files.
func checkWorkloadConfig(cfg *types.Config, src source, profileName, workloadName string) Check {
	if cfg == nil {
		return Check{
			Name:   "workload config",
			Status: Fail,
			Detail: "no qubesome config was loaded",
			Fix:    "Run qubesome from a directory with a qubesome config, or pass -git or -local to point at one.",
		}
	}

	if _, ok := cfg.Profiles[profileName]; !ok {
		names := make([]string, 0, len(cfg.Profiles))
		for n := range cfg.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)

		return Check{
			Name:   "workload config",
			Status: Fail,
			Detail: fmt.Sprintf("profile %q is not defined, known profiles: %s", profileName, strings.Join(names, ", ")),
			Fix:    "Check the profile name for typos, or add it to the config.",
		}
	}

	dir, err := workloadsDir(src, cfg.Profiles[profileName])
	if err != nil {
		return Check{
			Name:   "workload config",
			Status: Fail,
			Detail: fmt.Sprintf("could not resolve the profile's workloads dir: %s", err),
		}
	}

	names, err := workloadNames(dir)
	if err != nil {
		return Check{
			Name:   "workload config",
			Status: Fail,
			Detail: fmt.Sprintf("could not read %s: %s", dir, err),
			Fix:    "A profile's workloads live under its path. Create the directory, or fix the profile's path.",
		}
	}

	for _, n := range names {
		if n == workloadName {
			return Check{
				Name:   "workload config",
				Status: OK,
				Detail: fmt.Sprintf("workload %q is defined for profile %q", workloadName, profileName),
			}
		}
	}

	sort.Strings(names)

	known := strings.Join(names, ", ")
	if known == "" {
		known = fmt.Sprintf("none found under %s", dir)
	}

	return Check{
		Name:   "workload config",
		Status: Fail,
		Detail: fmt.Sprintf("workload %q is not defined for profile %q, known workloads: %s",
			workloadName, profileName, known),
		Fix: "Check the workload name for typos, or add a file for it under the profile's workloads directory.",
	}
}

// workloadsDir resolves the directory a profile's workload files live in,
// the way qubesome run resolves it.
//
// A profile's workloads descend from its path, which is not necessarily
// its name, so deriving the directory from the profile name finds nothing
// for any profile whose path differs from it.
func workloadsDir(src source, profile types.Profile) (string, error) {
	return files.WorkloadsDir(src.root, profile.Path)
}

// workloadNames lists the workloads defined in dir. A workload's name is
// its file's base name without its extension, matching how
// internal/profiles reads them.
func workloadNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}

		names = append(names, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
	}

	return names, nil
}

// loadWorkload reads and parses the workload's file, mirroring how
// qubesome run hydrates it, since that logic is not reachable here
// without an import cycle.
func loadWorkload(src source, profile types.Profile, workloadName string) (types.Workload, error) {
	dir, err := workloadsDir(src, profile)
	if err != nil {
		return types.Workload{}, err
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return types.Workload{}, err
	}
	defer root.Close()

	name := workloadName + ".yaml"
	path := filepath.Join(dir, name)

	data, err := root.ReadFile(name)
	if err != nil {
		return types.Workload{}, err
	}

	var w types.Workload
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Enforces that all YAML fields match struct fields exactly.
	if err := decoder.Decode(&w); err != nil {
		if errors.Is(err, io.EOF) {
			return types.Workload{}, fmt.Errorf("workload file %q is empty", path)
		}
		return types.Workload{}, err
	}

	w.Name = workloadName

	return w, nil
}

// checkWorkloadImage reports whether the workload's image is present
// locally. Its absence is a Warn, not a Fail, since qubesome pulls a
// missing image on start.
func checkWorkloadImage(env Env, bin, image string) Check {
	if _, err := env.Output(bin, "image", "inspect", image); err != nil {
		return Check{
			Name:   "workload image",
			Status: Warn,
			Detail: fmt.Sprintf("%s is not present locally", image),
			Fix:    fmt.Sprintf("It will be pulled on start, or pull it now with `%s pull %s`.", bin, image),
		}
	}

	return Check{
		Name:   "workload image",
		Status: OK,
		Detail: fmt.Sprintf("%s is present locally", image),
	}
}

// checkWorkloadProfileRunning reports whether the profile a workload
// needs is up, since a workload connects to its profile's display and
// has nowhere to attach to otherwise.
func checkWorkloadProfileRunning(env Env, bin, profileName string) Check {
	out, err := env.Output(bin, "ps", "--filter", "name=qubesome-"+profileName, "--format", "{{.Names}}")
	if err != nil {
		return Check{
			Name:   "profile running",
			Status: Fail,
			Detail: fmt.Sprintf("could not check whether the profile is running: %s", firstLine(string(out))),
			Fix:    fmt.Sprintf("Run `%s ps` directly to see the full error and act on it.", bin),
		}
	}

	if strings.TrimSpace(string(out)) == "" {
		return Check{
			Name:   "profile running",
			Status: Fail,
			Detail: "a workload needs its profile's display, and the profile is not running",
			Fix:    fmt.Sprintf("Start it with `qubesome start %s`.", profileName),
		}
	}

	return Check{
		Name:   "profile running",
		Status: OK,
		Detail: fmt.Sprintf("profile %q is running", profileName),
	}
}

// checkWorkloadHostAccess compares what the workload asks for against
// what the profile allows, naming exactly what was narrowed away, since
// a workload asking for something its profile does not grant is a
// common and confusing source of "it does not work".
func checkWorkloadHostAccess(w types.Workload, effective types.EffectiveWorkload) Check {
	var dropped []string
	var granted []string

	boolFields := []struct {
		name      string
		requested bool
		got       bool
	}{
		{"camera", w.HostAccess.Camera, effective.Workload.HostAccess.Camera},
		{"microphone", w.HostAccess.Microphone, effective.Workload.HostAccess.Microphone},
		{"speakers", w.HostAccess.Speakers, effective.Workload.HostAccess.Speakers},
		{"dbus", w.HostAccess.Dbus, effective.Workload.HostAccess.Dbus},
		{"varRunUser", w.HostAccess.VarRunUser, effective.Workload.HostAccess.VarRunUser},
		{"bluetooth", w.HostAccess.Bluetooth, effective.Workload.HostAccess.Bluetooth},
		{"mime", w.HostAccess.Mime, effective.Workload.HostAccess.Mime},
		{"seccompUnconfined", w.HostAccess.SeccompUnconfined, effective.Workload.HostAccess.SeccompUnconfined},
	}

	for _, f := range boolFields {
		if f.requested && !f.got {
			dropped = append(dropped, f.name)
		} else if f.requested {
			granted = append(granted, f.name)
		}
	}

	if w.HostAccess.Gpus != "" {
		if effective.Workload.HostAccess.Gpus == "" {
			dropped = append(dropped, fmt.Sprintf("gpus (requested %q)", w.HostAccess.Gpus))
		} else {
			granted = append(granted, "gpus")
		}
	}

	switch {
	case w.HostAccess.Network == "" || w.HostAccess.Network == "none":
		// Nothing requested, or the workload explicitly asked to have no
		// network, which ApplyProfile always honours. Neither is a grant
		// to report.
	case w.HostAccess.Network != effective.Workload.HostAccess.Network:
		dropped = append(dropped, fmt.Sprintf("network (requested %q, got %q)",
			w.HostAccess.Network, effective.Workload.HostAccess.Network))
	default:
		granted = append(granted, "network")
	}

	dropped = append(dropped, listDrops("path", w.HostAccess.Paths, effective.Workload.HostAccess.Paths)...)
	dropped = append(dropped, listDrops("device", w.HostAccess.Devices, effective.Workload.HostAccess.Devices)...)
	dropped = append(dropped, listDrops("usbDevice", w.HostAccess.USBDevices, effective.Workload.HostAccess.USBDevices)...)
	dropped = append(dropped, listDrops("capability", w.HostAccess.CapsAdd, effective.Workload.HostAccess.CapsAdd)...)

	if len(w.HostAccess.Paths) > 0 && len(dropped) == 0 {
		granted = append(granted, fmt.Sprintf("%d path(s)", len(w.HostAccess.Paths)))
	}
	if len(w.HostAccess.Devices) > 0 && len(effective.Workload.HostAccess.Devices) == len(w.HostAccess.Devices) {
		granted = append(granted, fmt.Sprintf("%d device(s)", len(w.HostAccess.Devices)))
	}
	if len(w.HostAccess.USBDevices) > 0 && len(effective.Workload.HostAccess.USBDevices) == len(w.HostAccess.USBDevices) {
		granted = append(granted, fmt.Sprintf("%d usb device(s)", len(w.HostAccess.USBDevices)))
	}

	if len(dropped) > 0 {
		return Check{
			Name:   "host access",
			Status: Warn,
			Detail: fmt.Sprintf("narrowed by the profile's hostAccess envelope: %s", strings.Join(dropped, ", ")),
			Fix:    "A workload's hostAccess is narrowed to what its profile allows. Add the matching field to the profile's hostAccess to grant it.",
		}
	}

	if len(granted) == 0 {
		return Check{
			Name:   "host access",
			Status: OK,
			Detail: "the workload does not request any host access",
		}
	}

	return Check{
		Name:   "host access",
		Status: OK,
		Detail: fmt.Sprintf("granted: %s", strings.Join(granted, ", ")),
	}
}

// listDrops names the entries requested that did not survive
// ApplyProfile's narrowing to the profile's allowlist.
func listDrops(label string, requested, got []string) []string {
	if len(requested) == 0 {
		return nil
	}

	gotSet := make(map[string]bool, len(got))
	for _, g := range got {
		gotSet[g] = true
	}

	var dropped []string
	for _, r := range requested {
		if !gotSet[r] {
			dropped = append(dropped, fmt.Sprintf("%s %q", label, r))
		}
	}

	return dropped
}

// checkWorkloadValidation reports on the effective workload's overall
// validity, catching anything the narrower checks above did not.
func checkWorkloadValidation(effective types.EffectiveWorkload) Check {
	if err := effective.Validate(); err != nil {
		return Check{
			Name:   "workload validation",
			Status: Fail,
			Detail: err.Error(),
		}
	}

	return Check{
		Name:   "workload validation",
		Status: OK,
		Detail: "the effective workload validates",
	}
}
