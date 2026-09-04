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
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
)

const (
	FileMode = 0o600
	DirMode  = 0o700
)

var (
	// ErrUnableGetSocketPath is an error returned when unable to get the socket path for a profile.
	ErrUnableGetSocketPath = errors.New("unable to get socket path for profile")

	// ErrMissingMappedPath is an error returned when the source of a mapped
	// path does not exist, and cannot be created by qubesome.
	ErrMissingMappedPath = errors.New("mapped path does not exist")
)

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

// ClientCookiePath returns the path to the client cookie file for the given profile.
func ClientCookiePath(profile string) (string, error) {
	base := RunUserQubesome()
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/.Xclient-cookie", profile))
}

func IsolatedRunUserPath(profile string) (string, error) {
	base := RunUserQubesome()
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/user", profile))
}

// ServerCookiePath returns the path to the server cookie file for the given profile.
func ServerCookiePath(profile string) (string, error) {
	base := RunUserQubesome()
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/.Xserver-cookie", profile))
}

// SocketPath returns the path to the socket file for the given profile.
func SocketPath(profile string) (string, error) {
	base := RunUserQubesome()
	return securejoin.SecureJoin(base, fmt.Sprintf("%s/qube.sock", profile))
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

	p, err := securejoin.SecureJoin(base, url)
	if err != nil {
		return "", fmt.Errorf("cannot get git dir path for %q: %w", url, err)
	}

	return p, nil
}

// WorkloadsDir returns the workloads directory path for a given Qubesome profile.
func WorkloadsDir(root, path string) (string, error) {
	return securejoin.SecureJoin(root, filepath.Join(path, "workloads"))
}

func FlatpakApps() string {
	return "/var/lib/flatpak/exports/share/applications"
}

func FlatpakIcons() string {
	return "/var/lib/flatpak/exports/share/icons/hicolor/scalable/apps"
}
