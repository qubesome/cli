package gpu

import "os/exec"

// Supported checks whether nvidia gpu sharing is supported by the system.
//
// At present it only checks whether nvidia-container-toolkit is in the PATH.
// In the future, it should attempt to run a container to confirm it is
// properly configured and useable.
func Supported() bool {
	path, err := exec.LookPath("nvidia-container-toolkit")
	return path != "" && err == nil
}
