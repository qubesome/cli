package doctor

import (
	"fmt"
	"strings"

	"github.com/qubesome/cli/internal/types"
)

// missingDevice is one host device a profile or workload asked for and
// did not get.
type missingDevice struct {
	// desc names what is missing, for the report.
	desc string

	// fatal says whether a container that asked for it refuses to start.
	// A device node and /dev/snd are handed to the container runner as
	// they are, so the runner fails when they are absent. A USB name
	// matching nothing, an absent camera and an absent GPU are left out
	// of the arguments instead, so the container starts without them.
	fatal bool
}

// missingDevices reports the host devices access asks for that are not
// present. It lists everything missing rather than stopping at the
// first, since a report naming one of three attached devices sends
// someone round the loop twice.
func missingDevices(env Env, access types.HostAccess) []missingDevice {
	var missing []missingDevice

	for _, d := range access.Devices {
		src, _, _, err := types.ParseDevice(d)
		if err != nil {
			src = d
		}

		if _, err := env.Stat(src); err != nil {
			missing = append(missing, missingDevice{desc: fmt.Sprintf("device %s", src), fatal: true})
		}
	}

	// USB names are resolved one at a time because a single name resolves
	// to several nodes. A key needs its bus node and the hidraw nodes of
	// its interfaces, so a count across the whole set would say something
	// was missing without saying which name.
	for _, u := range access.USBDevices {
		nodes, err := env.USBNamed([]string{u})
		if err != nil || len(nodes) == 0 {
			missing = append(missing, missingDevice{desc: fmt.Sprintf("usb device %s", u)})
		}
	}

	if access.Camera {
		matches, _ := env.Glob("/dev/video*")
		if len(matches) == 0 {
			missing = append(missing, missingDevice{desc: "camera, no /dev/video* found"})
		}
	}

	if access.Microphone || access.Speakers {
		if _, err := env.Stat("/dev/snd"); err != nil {
			missing = append(missing, missingDevice{desc: "audio, no /dev/snd found", fatal: true})
		}
	}

	if access.Gpus != "" {
		matches, _ := env.Glob("/dev/dri/renderD*")
		if len(matches) == 0 {
			missing = append(missing, missingDevice{desc: "gpu, no render node found"})
		}
	}

	return missing
}

// requestsDevices reports whether access asks for any host device at all.
func requestsDevices(access types.HostAccess) bool {
	return len(access.Devices) > 0 || len(access.USBDevices) > 0 ||
		access.Camera || access.Microphone || access.Speakers || access.Gpus != ""
}

// describe joins what is missing into one line.
func describe(missing []missingDevice) string {
	descs := make([]string, 0, len(missing))
	for _, m := range missing {
		descs = append(descs, m.desc)
	}

	return strings.Join(descs, ", ")
}

// checkProfileDevices reports the host devices a profile grants that are
// not present.
//
// A profile's hostAccess is an envelope its workloads are narrowed to,
// not a set of devices the profile container is given: the profile
// container is passed /dev/dri and its GPU parameters and nothing else.
// So a grant for a device that is not attached costs a workload the
// feature and never stops the profile from starting.
func checkProfileDevices(env Env, access types.HostAccess) Check {
	const name = "profile devices"

	if !requestsDevices(access) {
		return Check{
			Name:   name,
			Status: OK,
			Detail: "no host devices are granted",
		}
	}

	missing := missingDevices(env, access)
	if len(missing) == 0 {
		return Check{
			Name:   name,
			Status: OK,
			Detail: fmt.Sprintf("all %d granted host device(s) are present", countRequested(access)),
		}
	}

	return Check{
		Name:   name,
		Status: Warn,
		Detail: describe(missing),
		Fix: "These are grants for this profile's workloads, not devices the profile itself uses, " +
			"so the profile still starts. Attach them, or remove the grant from the config.",
	}
}

// checkWorkloadDevices reports the host devices a workload asks for that
// are not present.
//
// Severity is per device, because the container runner is not handed all
// of them the same way. What is passed through as it was written breaks
// the container when it is not there, and what qubesome resolves against
// the host is left out instead, which costs the workload a feature and
// nothing more. The check takes the severity of the worst thing in it.
func checkWorkloadDevices(env Env, access types.HostAccess) Check {
	const name = "workload devices"

	if !requestsDevices(access) {
		return Check{
			Name:   name,
			Status: OK,
			Detail: "no host devices are requested",
		}
	}

	missing := missingDevices(env, access)
	if len(missing) == 0 {
		return Check{
			Name:   name,
			Status: OK,
			Detail: fmt.Sprintf("all %d requested host device(s) are present", countRequested(access)),
		}
	}

	for _, m := range missing {
		if m.fatal {
			return Check{
				Name:   name,
				Status: Fail,
				Detail: describe(missing),
				Fix:    "The container will not start without them. Attach the device, or remove the request from the workload's hostAccess.",
			}
		}
	}

	if len(missing) == 1 && strings.HasPrefix(missing[0].desc, "gpu") {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: describe(missing),
			Fix:    "The workload will fall back to software rendering, which works but is slower.",
		}
	}

	return Check{
		Name:   name,
		Status: Warn,
		Detail: describe(missing),
		Fix: "These are left out of the container's arguments, so the workload starts without them. " +
			"Attach the device, or remove the request from the workload's hostAccess.",
	}
}

// countRequested totals the individual grants in access, for the OK
// detail message.
func countRequested(access types.HostAccess) int {
	count := len(access.Devices) + len(access.USBDevices)

	if access.Camera {
		count++
	}
	if access.Microphone || access.Speakers {
		count++
	}
	if access.Gpus != "" {
		count++
	}

	return count
}
