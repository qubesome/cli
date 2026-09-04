package gpu

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ErrNoGPU is returned when no render node is found on the host.
var ErrNoGPU = errors.New("no GPU render node found")

const cdiVersion = "0.6.0"

// Spec is the subset of the Container Device Interface specification
// required to share a Mesa based GPU with a workload.
type Spec struct {
	CDIVersion string   `yaml:"cdiVersion"`
	Kind       string   `yaml:"kind"`
	Devices    []Device `yaml:"devices"`
}

type Device struct {
	Name           string         `yaml:"name"`
	ContainerEdits ContainerEdits `yaml:"containerEdits"`
}

type ContainerEdits struct {
	DeviceNodes []DeviceNode `yaml:"deviceNodes,omitempty"`
	Mounts      []Mount      `yaml:"mounts,omitempty"`
}

type DeviceNode struct {
	Path string `yaml:"path"`
}

type Mount struct {
	HostPath      string   `yaml:"hostPath"`
	ContainerPath string   `yaml:"containerPath"`
	Options       []string `yaml:"options"`
}

// NewSpec builds a CDI spec sharing the render nodes and the Vulkan hardware
// drivers found under root. Root is only overridden by tests.
func NewSpec(root string) (Spec, error) {
	nodes, err := deviceNodes(root)
	if err != nil {
		return Spec{}, err
	}

	mounts, err := vulkanMounts(root)
	if err != nil {
		return Spec{}, err
	}

	return Spec{
		CDIVersion: cdiVersion,
		Kind:       CDIKind,
		Devices: []Device{
			{
				Name: "all",
				ContainerEdits: ContainerEdits{
					DeviceNodes: nodes,
					Mounts:      mounts,
				},
			},
		},
	}, nil
}

// YAML renders the spec in the format runners load it from.
func (s Spec) YAML() ([]byte, error) {
	data, err := yaml.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CDI spec: %w", err)
	}

	return data, nil
}

// Write renders the spec into path.
func (s Spec) Write(path string) error {
	data, err := s.YAML()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to ensure CDI dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // runners read the spec as other users.
		return fmt.Errorf("failed to write CDI spec: %w", err)
	}

	return nil
}

func deviceNodes(root string) ([]DeviceNode, error) {
	paths, err := filepath.Glob(filepath.Join(root, "dev/dri/*"))
	if err != nil {
		return nil, fmt.Errorf("failed to list render nodes: %w", err)
	}

	var (
		nodes  []DeviceNode
		render bool
	)

	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}

		name := filepath.Base(p)
		if strings.HasPrefix(name, "renderD") {
			render = true
		}

		nodes = append(nodes, DeviceNode{Path: containerPath(root, p)})
	}

	if !render {
		return nil, ErrNoGPU
	}

	return nodes, nil
}

func vulkanMounts(root string) ([]Mount, error) {
	icds, err := filepath.Glob(filepath.Join(root, "usr/share/vulkan/icd.d/*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list Vulkan ICDs: %w", err)
	}

	var mounts []Mount
	for _, icd := range icds {
		// lavapipe is a software rasteriser. The workload image ships its own,
		// and sharing the host one would gain nothing.
		if strings.HasPrefix(filepath.Base(icd), "lvp_") {
			continue
		}

		lib, err := icdLibrary(root, icd)
		if err != nil {
			slog.Warn("skipping Vulkan ICD", "icd", icd, "error", err)
			continue
		}

		mounts = append(mounts, mount(root, icd), mount(root, lib))
	}

	return mounts, nil
}

func icdLibrary(root, icd string) (string, error) {
	data, err := os.ReadFile(icd)
	if err != nil {
		return "", fmt.Errorf("failed to read ICD: %w", err)
	}

	// Field names are defined by the Khronos ICD manifest format.
	//nolint:tagliatelle
	var parsed struct {
		ICD struct {
			LibraryPath string `json:"library_path"`
		} `json:"ICD"`
	}

	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse ICD: %w", err)
	}

	lib := parsed.ICD.LibraryPath
	if lib == "" {
		return "", errors.New("ICD has no library_path")
	}

	// Relative paths are resolved against the directory holding the ICD.
	if filepath.IsAbs(lib) {
		lib = filepath.Join(root, lib)
	} else {
		lib = filepath.Join(filepath.Dir(icd), lib)
	}

	if _, err := os.Stat(lib); err != nil {
		return "", fmt.Errorf("failed to find ICD library %q: %w", lib, err)
	}

	return lib, nil
}

func mount(root, path string) Mount {
	return Mount{
		HostPath:      path,
		ContainerPath: containerPath(root, path),
		Options:       []string{"ro", "bind"},
	}
}

func containerPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}

	return "/" + rel
}
