package gpu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// CDIKind is the CDI kind qubesome generates for Mesa based GPUs.
	CDIKind = "qubesome.dev/gpu"

	// CDISpecName is the file name of the generated CDI spec.
	CDISpecName = "qubesome-gpu.yaml"
)

// cdiSpecDirs are the directories container runners load CDI specs from.
var cdiSpecDirs = []string{"/etc/cdi", "/var/run/cdi"}

// Params returns the runner arguments that give a container access to the
// host GPU, and whether a GPU was detected at all. A GPU may be supported
// without requiring any additional argument.
func Params(runner string) ([]string, bool) {
	return params("/", runner, exec.LookPath)
}

func params(root, runner string, lookPath func(string) (string, error)) ([]string, bool) {
	if path, _ := lookPath("nvidia-container-toolkit"); path != "" {
		if runner == "podman" {
			return []string{"--device=nvidia.com/gpu=all"}, true
		}
		return []string{"--gpus=all"}, true
	}

	// A generated CDI spec shares the host Vulkan drivers, so it is preferred
	// over the plain render nodes below.
	if cdiSpecRegistered(root) {
		return []string{"--device=" + CDIKind + "=all"}, true
	}

	// AMD GPU based on AMD Kernel Fusion Driver.
	if _, err := os.Stat(filepath.Join(root, "dev/kfd")); err == nil {
		return []string{"--device=/dev/kfd"}, true
	}

	// Mesa drivers only need the render nodes, which are always shared with
	// workloads. No additional argument is required as long as the workload
	// image carries the hardware ICDs.
	if nodes, _ := filepath.Glob(filepath.Join(root, "dev/dri/renderD*")); len(nodes) > 0 {
		return nil, true
	}

	return nil, false
}

func cdiSpecRegistered(root string) bool {
	for _, dir := range cdiSpecDirs {
		if _, err := os.Stat(filepath.Join(root, dir, CDISpecName)); err == nil {
			return true
		}
	}

	return false
}

// SpecPath returns the location the CDI spec is written to.
func SpecPath() string {
	return filepath.Join(cdiSpecDirs[0], CDISpecName)
}

// Setup generates a CDI spec sharing the host GPU and its Vulkan drivers,
// and writes it where container runners load it from.
func Setup() error {
	spec, err := NewSpec("/")
	if err != nil {
		return err
	}

	return spec.Write(SpecPath())
}

// Describe reports how GPU access is shared with workloads.
func Describe(runner string) string {
	params, ok := Params(runner)
	if !ok {
		return "no GPU detected"
	}

	if len(params) == 0 {
		return "GPU shared through the render nodes in /dev/dri. Workload images must carry their own Vulkan drivers."
	}

	return "GPU shared with: " + strings.Join(params, " ")
}
