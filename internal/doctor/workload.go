package doctor

import (
	"fmt"
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
	configCheck := checkWorkloadConfig(cfg, profileName, workloadName)
	if configCheck.Status != OK {
		// Every later check depends on a valid, locatable workload.
		// Returning here stops them from failing for the same reason
		// and burying it.
		return []Check{configCheck}
	}

	profile := cfg.Profiles[profileName]
	bin := files.ContainerRunnerBinary(runner)

	w, err := loadWorkload(cfg, profileName, workloadName)
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
		checkDevices(env, "workload devices", effective.Workload.HostAccess),
		checkWorkloadPaths(env, effective),
		checkWorkloadValidation(effective),
	}
}

// checkWorkloadConfig confirms that a config was loaded, that the
// requested profile exists, and that the requested workload can be found
// among the profile's workload files.
func checkWorkloadConfig(cfg *types.Config, profileName, workloadName string) Check {
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

	workloadFiles, err := cfg.WorkloadFiles()
	if err != nil {
		return Check{
			Name:   "workload config",
			Status: Fail,
			Detail: fmt.Sprintf("could not list workload files: %s", err),
		}
	}

	names := workloadNames(workloadFiles, profileName)
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

	return Check{
		Name:   "workload config",
		Status: Fail,
		Detail: fmt.Sprintf("workload %q is not defined for profile %q, known workloads: %s",
			workloadName, profileName, strings.Join(names, ", ")),
		Fix: "Check the workload name for typos, or add a file for it under the profile's workloads directory.",
	}
}

// workloadNames extracts the workload names defined for profileName out
// of the paths WorkloadFiles returned. A workload's name is its file's
// base name without its extension, matching how internal/profiles reads
// them, and a path belongs to profileName when its parent directory is
// that profile's "workloads" directory.
func workloadNames(workloadFiles []string, profileName string) []string {
	var names []string

	for _, f := range workloadFiles {
		dir := filepath.Dir(f)
		if filepath.Base(dir) != "workloads" {
			continue
		}
		if filepath.Base(filepath.Dir(dir)) != profileName {
			continue
		}

		names = append(names, strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
	}

	return names
}

// loadWorkload reads and parses the workload's file, mirroring how
// internal/profiles hydrates it, since that logic is not reachable here
// without an import cycle.
func loadWorkload(cfg *types.Config, profileName, workloadName string) (types.Workload, error) {
	workloadFiles, err := cfg.WorkloadFiles()
	if err != nil {
		return types.Workload{}, err
	}

	for _, f := range workloadFiles {
		dir := filepath.Dir(f)
		if filepath.Base(dir) != "workloads" || filepath.Base(filepath.Dir(dir)) != profileName {
			continue
		}

		if strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)) != workloadName {
			continue
		}

		data, err := os.ReadFile(f)
		if err != nil {
			return types.Workload{}, err
		}

		var w types.Workload
		if err := yaml.Unmarshal(data, &w); err != nil {
			return types.Workload{}, err
		}

		w.Name = workloadName

		return w, nil
	}

	return types.Workload{}, fmt.Errorf("workload %q not found for profile %q", workloadName, profileName)
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
	out, _ := env.Output(bin, "ps", "--filter", "name=qubesome-"+profileName, "--format", "{{.Names}}")

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
		{"privileged", w.HostAccess.Privileged, effective.Workload.HostAccess.Privileged},
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

	if w.HostAccess.Network != "" && w.HostAccess.Network != "none" &&
		w.HostAccess.Network != effective.Workload.HostAccess.Network {
		dropped = append(dropped, fmt.Sprintf("network (requested %q, got %q)",
			w.HostAccess.Network, effective.Workload.HostAccess.Network))
	} else if w.HostAccess.Network != "" {
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

// checkWorkloadPaths reports on the host source side of the effective
// workload's mapped paths.
func checkWorkloadPaths(env Env, effective types.EffectiveWorkload) Check {
	paths := effective.Workload.HostAccess.Paths
	if len(paths) == 0 {
		return Check{
			Name:   "workload paths",
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
			Name:   "workload paths",
			Status: Warn,
			Detail: fmt.Sprintf("missing on the host: %s", strings.Join(missing, ", ")),
			Fix:    "These are skipped with a warning at start. Create them, or remove them from the config.",
		}
	}

	return Check{
		Name:   "workload paths",
		Status: OK,
		Detail: fmt.Sprintf("all %d mapped path(s) are present", len(paths)),
	}
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
