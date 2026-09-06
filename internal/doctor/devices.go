package doctor

import (
	"fmt"
	"strings"

	"github.com/qubesome/cli/internal/types"
)

// checkDevices reports the host devices a profile or workload asks for
// that are not present.
//
// name is the check's name, which differs between a profile and a
// workload, and subject names what asked, for the detail text.
func checkDevices(env Env, name string, access types.HostAccess) Check {
	requested := len(access.Devices) > 0 || len(access.USBDevices) > 0 ||
		access.Camera || access.Microphone || access.Speakers || access.Gpus != ""

	if !requested {
		return Check{
			Name:   name,
			Status: OK,
			Detail: "no host devices are requested",
		}
	}

	var missing []string
	gpuMissing := false

	for _, d := range access.Devices {
		src, _, _, err := types.ParseDevice(d)
		if err != nil {
			src = d
		}

		if _, err := env.Stat(src); err != nil {
			missing = append(missing, fmt.Sprintf("device %s", src))
		}
	}

	for _, u := range access.USBDevices {
		nodes, err := env.USBNamed([]string{u})
		if err != nil || len(nodes) == 0 {
			missing = append(missing, fmt.Sprintf("usb device %s", u))
		}
	}

	if access.Camera {
		matches, _ := env.Glob("/dev/video*")
		if len(matches) == 0 {
			missing = append(missing, "camera, no /dev/video* found")
		}
	}

	if access.Microphone || access.Speakers {
		if _, err := env.Stat("/dev/snd"); err != nil {
			missing = append(missing, "audio, no /dev/snd found")
		}
	}

	if access.Gpus != "" {
		matches, _ := env.Glob("/dev/dri/renderD*")
		if len(matches) == 0 {
			missing = append(missing, "gpu, no render node found")
			gpuMissing = true
		}
	}

	if len(missing) == 0 {
		return Check{
			Name:   name,
			Status: OK,
			Detail: fmt.Sprintf("all %d requested host device(s) are present", countRequested(access)),
		}
	}

	// A GPU alone is degraded, not broken, matching how the environment
	// check treats a missing render node. Anything else missing makes
	// this a Fail, including when the GPU is missing alongside it.
	if gpuMissing && len(missing) == 1 {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: strings.Join(missing, ", "),
			Fix:    "The workload will fall back to software rendering, which works but is slower.",
		}
	}

	return Check{
		Name:   name,
		Status: Fail,
		Detail: strings.Join(missing, ", "),
		Fix:    "The container will not start without them. Attach the device, or remove the grant from the config.",
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
