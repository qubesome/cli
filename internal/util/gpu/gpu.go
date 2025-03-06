package gpu

import (
	"os"
	"os/exec"
)

// Supported checks whether GPU sharing is supported by the system, based
// on either NVidia or AMD toolkits being instead.
//
// At present it only checks whether nvidia-container-toolkit or
// rocm-smi are in the PATH.
// In the future, it should attempt to run a container to confirm it is
// properly configured and useable.
func Supported(runner string) (string, bool) {
	if path, _ := exec.LookPath("nvidia-container-toolkit"); path != "" {
		if runner == "podman" {
			return "--device=nvidia.com/gpu=all", true
		}
		return "--gpus=all", true
	}
	// AMD GPU based on AMD Kernel Fusion Driver.
	if _, err := os.Stat("/dev/kfd"); err == nil {
		return "--device=/dev/kfd", true
	}
	return "", false
}
