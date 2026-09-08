package doctor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/qubesome/cli/internal/types"
	envutil "github.com/qubesome/cli/internal/util/env"
)

// primeExpansion registers the variables that profile and workload paths
// are written against, exactly as a profile start registers them.
//
// Without it every ${GITDIR} and every external drive label stays
// literal, so paths that are present on the host are reported as missing
// and a workload's paths are narrowed against unexpanded profile grants.
// A drive is registered whether or not it is mounted, since a report that
// names the path someone is looking for is more use than one that does
// not, and the drive check reports the mount separately.
func primeExpansion(src source, profile types.Profile) {
	_ = envutil.Update("GITDIR", src.gitDir)

	for _, d := range profile.ExternalDrives {
		label, _, mount, err := parseExternalDrive(d)
		if err != nil {
			continue
		}

		envutil.Add(label, mount)
	}
}

// parseExternalDrive splits an externalDrives entry into the label its
// mountpoint is exported under, the device that must be mounted, and
// where it must be mounted. The format is the one a profile start
// enforces, so doctor rejects exactly what it rejects.
func parseExternalDrive(entry string) (label, device, mount string, err error) {
	parts := strings.Split(entry, ":")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("want label:device:mountpoint")
	}

	return parts[0], parts[1], parts[2], nil
}

// mappedSource returns the host side of a mapped path, expanded.
//
// The split comes before the expansion because that is the order a start
// uses, and an expanded value containing a colon would otherwise be cut
// in the middle.
func mappedSource(entry string) string {
	src := entry
	if i := strings.Index(entry, ":"); i >= 0 {
		src = entry[:i]
	}

	return envutil.Expand(src)
}

// missingMappedPaths returns the sources qubesome would skip, judged the
// way files.EnsureMappedDir judges them. An existing source is fine
// whatever its type, and a missing one that declares itself a directory,
// by ending with a separator, is created when its parent dir is already
// there.
func missingMappedPaths(env Env, paths []string) []string {
	var missing []string

	for _, p := range paths {
		src := mappedSource(p)

		if _, err := env.Lstat(src); err == nil {
			continue
		}

		if strings.HasSuffix(src, string(filepath.Separator)) {
			parent := filepath.Dir(filepath.Clean(src))
			if fi, err := env.Stat(parent); err == nil && fi.IsDir() {
				continue
			}
		}

		missing = append(missing, src)
	}

	return missing
}

// checkMappedPaths reports on the host source side of a set of mapped
// paths. Missing sources are a Warn because a start skips them with a
// warning rather than failing.
func checkMappedPaths(env Env, name string, paths []string) Check {
	if len(paths) == 0 {
		return Check{
			Name:   name,
			Status: OK,
			Detail: "no paths are configured",
		}
	}

	missing := missingMappedPaths(env, paths)
	if len(missing) > 0 {
		return Check{
			Name:   name,
			Status: Warn,
			Detail: fmt.Sprintf("missing on the host: %s", strings.Join(missing, ", ")),
			Fix:    "These are skipped with a warning at start. Create them, or remove them from the config.",
		}
	}

	return Check{
		Name:   name,
		Status: OK,
		Detail: fmt.Sprintf("all %d mapped path(s) are present", len(paths)),
	}
}
