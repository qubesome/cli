package podman

import (
	"fmt"
	"path/filepath"

	"github.com/qubesome/cli/internal/types"
)

// runUserInput describes what a workload needs from the profile's runtime
// directory.
type runUserInput struct {
	// UserDir is the profile's isolated /run/user directory on the host.
	UserDir string

	// ProfileDir is the profile's directory on the host, which holds the
	// generated machine-id.
	ProfileDir string

	// ShmDir is this workload's own /dev/shm directory on the host, from
	// files.WorkloadShmPath.
	ShmDir string

	types.HostAccess
}

// runUserParams returns the runner arguments and mounts that give a
// workload its runtime directory and shared memory.
//
// It matches the docker runner, with the :z relabel suffix podman needs on
// the mounts it shares with the host. The two are kept separate rather than
// shared, because the suffixes are the kind of difference that a shared
// helper would have to grow a parameter for.
//
// /dev/shm is per workload. It used to be one directory shared by the
// profile and all of its workloads, which was a channel between them for
// no benefit: MIT-SHM is disabled on the profile display, so nothing
// passes shared memory through the X server.
func runUserParams(in runUserInput) ([]string, []string) {
	var args, paths []string

	paths = append(paths, fmt.Sprintf("-v=%s:/dev/shm:z", in.ShmDir))

	if in.Dbus || in.Bluetooth || in.VarRunUser {
		args = append(args, "-v=/run/user/1000:/run/user/1000:z")
		return args, paths
	}

	paths = append(paths, fmt.Sprintf("-v=%s:/run/user/1000:z", in.UserDir))
	paths = append(paths, fmt.Sprintf("-v=%s:/etc/machine-id:ro,z",
		filepath.Join(in.ProfileDir, "machine-id")))

	return args, paths
}
