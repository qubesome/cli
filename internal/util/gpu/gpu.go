package gpu

import (
	"errors"
	"fmt"
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

// cdiSpecDirs are the directories a CDI spec is written to. Nothing
// qubesome runs reads them any more, since bwrap does not resolve a CDI
// kind, but the generated spec is still what gpu setup writes and what a
// container runner elsewhere on the host would pick up.
var cdiSpecDirs = []string{"/etc/cdi", "/var/run/cdi"}

// SandboxEdits returns the device nodes and mounts a bwrap sandbox needs
// for GPU access.
//
// Params returns runner arguments naming a CDI kind or a runner feature,
// and bwrap resolves neither. The CDI spec was always the real content, so
// this returns it directly for the caller to render as --dev-bind and
// --ro-bind.
func SandboxEdits(root string) ([]DeviceNode, []Mount, error) {
	spec, err := NewSpec(root)
	if err != nil {
		return nil, nil, err
	}

	// NewSpec always returns exactly one device named "all" when it
	// succeeds, so this is unreachable today. It stays as a guard against
	// that invariant changing underneath this function.
	if len(spec.Devices) == 0 {
		return nil, nil, ErrNoGPU
	}

	edits := spec.Devices[0].ContainerEdits

	return edits.DeviceNodes, edits.Mounts, nil
}

// NvidiaToolkitPresent reports whether the nvidia container toolkit is
// installed.
//
// The toolkit injects driver libraries into the container through a runner
// hook, which bwrap has no equivalent of, so a profile on an nvidia GPU
// cannot be given hardware rendering this way. The caller warns and
// continues without a GPU rather than starting one that silently does not
// work.
func NvidiaToolkitPresent() bool {
	return nvidiaToolkitPresent(exec.LookPath)
}

func nvidiaToolkitPresent(lookPath func(string) (string, error)) bool {
	path, _ := lookPath("nvidia-container-toolkit")
	return path != ""
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

// Describe reports how GPU access is shared with a sandbox.
//
// It used to render the arguments Params produced for a container
// runner, which meant it answered for a runtime qubesome no longer has.
// A host with an AMD card was told "GPU shared with: --device=/dev/kfd",
// naming a flag nothing passes any more. It now reports what a sandbox
// is actually given.
func Describe() string {
	return describe("/", exec.LookPath)
}

func describe(root string, lookPath func(string) (string, error)) string {
	// The toolkit shares a GPU by injecting driver libraries through a
	// container runner hook, and bwrap has no equivalent, so a profile
	// on such a host is started without a GPU rather than with one that
	// silently does not work. Saying so is more use than describing the
	// devices it would otherwise have had.
	if nvidiaToolkitPresent(lookPath) {
		return "nvidia container toolkit found, which a sandbox cannot use: " +
			"it shares a GPU through a container runner hook. Profiles and " +
			"workloads on this host run without a GPU."
	}

	nodes, mounts, err := SandboxEdits(root)
	if err != nil {
		if errors.Is(err, ErrNoGPU) {
			return "no GPU detected"
		}

		return "cannot tell how the GPU would be shared: " + err.Error()
	}

	paths := make([]string, 0, len(nodes))
	for _, n := range nodes {
		paths = append(paths, n.Path)
	}

	desc := "GPU shared through " + strings.Join(paths, " ")
	if len(mounts) == 0 {
		// Nothing from the host is mounted, so the drivers have to come
		// from the image.
		return desc + ". Images must carry their own Vulkan drivers, or run `qubesome gpu setup`."
	}

	return fmt.Sprintf("%s, with %d host driver path(s) shared read-only.", desc, len(mounts))
}
