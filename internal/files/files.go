// Package files centralises location paths used in Qubesome.
//
// Key locations:
// - ~/.qubesome: default location for persistent files.
// - ~/.qubesome/images-last-checked: file that stores when images were last checked.
// - ~/.qubesome/run: root of ephemeral files.
// - ~/.qubesome/git/<git-url>/<path>: where git repositories
// are cloned to.
package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	FileMode = 0o600
	DirMode  = 0o700

	// nameMax bounds a single path component at what a filesystem takes
	// for one, so a name cannot fail a syscall for its length alone.
	nameMax = 255
)

var (
	// ErrUnableGetSocketPath is an error returned when unable to get the socket path for a profile.
	ErrUnableGetSocketPath = errors.New("unable to get socket path for profile")

	// ErrMissingMappedPath is an error returned when the source of a mapped
	// path does not exist, and cannot be created by qubesome.
	ErrMissingMappedPath = errors.New("mapped path does not exist")

	// ErrUnsafePath is an error returned when a name or a relative path
	// would reach outside the directory it is joined to.
	ErrUnsafePath = errors.New("unsafe path")

	// nameRegex bounds the path components built from a profile or a
	// workload name. It repeats what internal/types validates a name
	// against, because types imports this package and cannot be imported
	// back.
	nameRegex = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`)
)

// ValidateName reports whether name can stand as a single path component.
//
// The alphabet leaves out the separator and the dot, so a name can neither
// descend nor climb, and a name that is checked once here holds for as
// long as it is a name. kind names the field in the error.
func ValidateName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%w: %s is empty", ErrUnsafePath, kind)
	}
	if len(name) > nameMax {
		return fmt.Errorf("%w: %s is longer than %d bytes", ErrUnsafePath, kind, nameMax)
	}
	if !nameRegex.MatchString(name) {
		return fmt.Errorf("%w: %s %q does not match %s", ErrUnsafePath, kind, name, nameRegex)
	}
	return nil
}

// JoinRel joins rel below base.
//
// rel has to be relative and no component of it may be "..", so the result
// always descends from base. An empty rel, or ".", is base itself, which is
// how a profile whose files sit at the root of its config is spelled.
//
// An escaping rel is refused rather than clamped into base. A join that
// clamps returns a path the caller did not ask for, with nothing to say it
// was rewritten, and these results go on to be read, created and bind
// mounted.
//
// Symlinks already under base are not resolved, so the result can still
// point through one. Every base here is either the qubesome run directory
// or the directory a config was sourced from, and whoever can plant a
// symlink in one of those also writes the config that says which images
// run and which host paths they are given.
func JoinRel(base, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q is absolute", ErrUnsafePath, rel)
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("%w: %q leaves %q", ErrUnsafePath, rel, base)
		}
	}
	return filepath.Join(base, rel), nil
}

// EnsureMappedDir prepares the host side of a bind mount source.
//
// Existing paths are left untouched, whatever their type. A missing src
// is only created when it declares itself a directory, by ending with a
// path separator, and its parent dir is already present. Any other
// missing src returns ErrMissingMappedPath, as container runners create
// missing bind mount sources as root-owned dirs, which is wrong for file
// mappings and masks unmounted mount points. Errors other than the path
// being absent are returned as they are.
func EnsureMappedDir(src string) error {
	_, err := os.Lstat(src)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if !strings.HasSuffix(src, string(filepath.Separator)) {
		return ErrMissingMappedPath
	}

	dir := filepath.Clean(src)
	parent := filepath.Dir(dir)

	fi, err := os.Stat(parent)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: parent dir %q is not present", ErrMissingMappedPath, parent)
		}
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%w: parent %q is not a dir", ErrMissingMappedPath, parent)
	}

	return os.Mkdir(dir, DirMode)
}

// QubesomeDir returns the root directory where Qubesome configuration is stored.
func QubesomeDir() string {
	return os.ExpandEnv("${HOME}/.qubesome")
}

// QubesomeConfig returns the default qubesome config file path.
func QubesomeConfig() string {
	return filepath.Join(QubesomeDir(), "qubesome.config")
}

// ProfileConfig returns the profile config file path. This will be
// a symlink to the actual profile which is sourced within the Git
// repository.
func ProfileConfig(profile string) string {
	return filepath.Join(RunUserQubesome(), fmt.Sprintf("%s.config", profile))
}

// ImagesLastCheckedPath returns the file path for the file that records
// when images where last checked.
func ImagesLastCheckedPath() string {
	return filepath.Join(QubesomeDir(), "images-last-checked")
}

// RunUserQubesome returns the path to the user-specific qubesome directory.
func RunUserQubesome() string {
	return filepath.Join(QubesomeDir(), "run")
}

// profileRunDir returns a profile's directory under the qubesome run
// directory, refusing a profile name that is not a single path component.
func profileRunDir(profile string) (string, error) {
	if err := ValidateName("profile name", profile); err != nil {
		return "", err
	}
	return filepath.Join(RunUserQubesome(), profile), nil
}

// ClientCookiePath returns the path to the client cookie file for the given profile.
func ClientCookiePath(profile string) (string, error) {
	dir, err := profileRunDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".Xclient-cookie"), nil
}

func IsolatedRunUserPath(profile string) (string, error) {
	dir, err := profileRunDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "user"), nil
}

// WorkloadShmPath returns the /dev/shm directory for one workload of a
// profile. Each workload gets its own, so shared memory is not a channel
// between them.
//
// It deliberately sits beside the profile's runtime directory rather than
// inside it. IsolatedRunUserPath is mounted into every workload, so a
// workload's shared memory placed under it would be reachable by all of
// its siblings, and every container runs as the same uid, so permissions
// would not help.
func WorkloadShmPath(profile, workload string) (string, error) {
	dir, err := profileRunDir(profile)
	if err != nil {
		return "", err
	}
	if err := ValidateName("workload name", workload); err != nil {
		return "", err
	}
	return filepath.Join(dir, "shm", workload), nil
}

// ServerCookiePath returns the path to the server cookie file for the given profile.
func ServerCookiePath(profile string) (string, error) {
	dir, err := profileRunDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".Xserver-cookie"), nil
}

// SocketPath returns the path to the socket file for the given profile.
func SocketPath(profile string) (string, error) {
	dir, err := profileRunDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "qube.sock"), nil
}

func ProfileDir(profile string) string {
	base := RunUserQubesome()
	return filepath.Join(base, profile)
}

// InProfileSocketPath returns the path to the socket when running inside the profile
// container.
func InProfileSocketPath() string {
	return "/tmp/qube.sock"
}

// GitRoot returns the root directory for git repositories.
func GitRoot() string {
	return filepath.Join(RunUserQubesome(), "git")
}

// GitDirPath returns the path to the git directory for the given URL.
func GitDirPath(url string) (string, error) {
	if strings.HasPrefix(url, "~") {
		if len(url) > 1 && url[1] == '/' {
			return os.ExpandEnv("${HOME}" + url[1:]), nil
		}
	}
	if strings.HasPrefix(url, "/") {
		return url, nil
	}

	base := GitRoot()

	url = strings.ReplaceAll(url, ":", "/")
	url = strings.ReplaceAll(url, "git@", "")

	p, err := JoinRel(base, url)
	if err != nil {
		return "", fmt.Errorf("cannot get git dir path for %q: %w", url, err)
	}

	// JoinRel reads an empty or dot path as the base itself. A URL that
	// names no directory of its own would make the whole git root one
	// repository's clone.
	if p == base {
		return "", fmt.Errorf("cannot get git dir path for %q: %w: it names no repository", url, ErrUnsafePath)
	}

	return p, nil
}

// WorkloadsDir returns the workloads directory path for a given Qubesome
// profile. An empty path is a profile whose files sit at root itself.
func WorkloadsDir(root, path string) (string, error) {
	dir, err := JoinRel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workloads"), nil
}

func FlatpakApps() string {
	return "/var/lib/flatpak/exports/share/applications"
}

func FlatpakIcons() string {
	return "/var/lib/flatpak/exports/share/icons/hicolor/scalable/apps"
}
