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

// JoinProfilePath joins a profile's configured path below base.
//
// An absolute profile path means one of two things, and both appear in
// real use. A path under base is a full path to a directory of the
// config tree, and is taken relative to it. A path that is not under
// base is one written rooted at the configuration, so "/personal" names
// the personal directory of that tree rather than a directory at the
// root of the disk. Every profile in a real configuration is written
// the second way.
//
// Neither convention was ever stated. SecureJoin clamped a path that
// left its base back into it, which covered the rooted form, and
// callers ran filepath.Rel against the config root first, which covered
// the under-base form. Removing the clamp removed half of it and left
// the other half looking like dead code, so both are spelled out here.
//
// Either way the result is under base, which is the property that
// matters. A rooted path naming something outside the tree, "/etc" for
// instance, resolves to base/etc and then fails to exist, rather than
// reaching /etc.
func JoinProfilePath(base, p string) (string, error) {
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(base, p)
		if err != nil || escapes(rel) {
			// Not under base, so the leading separator names the config
			// tree. Drop it and treat the rest as relative.
			rel = strings.TrimPrefix(p, string(filepath.Separator))
		}
		p = rel
	}

	return JoinRel(base, p)
}

// escapes reports whether a relative path starts by leaving its base. A
// leading ".." component is the only way it can, and it is compared as a
// component so that a directory named "..cache" is not mistaken for one.
func escapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// agentSocketName is the supervisor's socket, inside the directory a
// workload's sandbox has it bound at. The host and the sandbox build the
// same path from opposite ends, so they share the name.
const agentSocketName = "agent.sock"

// WorkloadAgentDir returns the host directory holding the supervisor
// socket of one workload of a profile.
//
// Like WorkloadShmPath it sits beside the profile's runtime directory
// rather than inside it, and for the same reason: IsolatedRunUserPath is
// mounted into every workload of the profile, so a socket under it would
// be reachable from every sibling, and every workload runs as the same
// uid, so permissions would not separate them. A directory per workload
// is what keeps a supervisor reachable only from its own sandbox.
func WorkloadAgentDir(profile, workload string) (string, error) {
	dir, err := profileRunDir(profile)
	if err != nil {
		return "", err
	}
	if err := ValidateName("workload name", workload); err != nil {
		return "", err
	}
	return filepath.Join(dir, "agent", workload), nil
}

// WorkloadAgentSocket returns the host path of a workload supervisor's
// socket.
func WorkloadAgentSocket(profile, workload string) (string, error) {
	dir, err := WorkloadAgentDir(profile, workload)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, agentSocketName), nil
}

// InWorkloadAgentDir returns the path WorkloadAgentDir is bound at inside
// the workload's sandbox.
func InWorkloadAgentDir() string {
	return "/run/qubesome"
}

// InWorkloadAgentSocket returns the path the supervisor listens on inside
// the workload's sandbox.
func InWorkloadAgentSocket() string {
	return filepath.Join(InWorkloadAgentDir(), agentSocketName)
}

const (
	// vmVsockSocketName is firecracker's host side vsock socket, the one
	// file a console reaches a guest through.
	vmVsockSocketName = "vsock.sock"

	// vmAPISocketName is firecracker's control socket. See VMAPISocket.
	vmAPISocketName = "api.sock"
)

// VMRuntimeDir returns the host directory holding everything one microVM
// of a profile needs while it is up.
//
// It sits beside the profile's runtime directory for the reason
// WorkloadAgentDir gives: IsolatedRunUserPath is bound into every
// workload of the profile, so anything under it is reachable from all of
// them, and a machine's control socket is the last thing that should be.
func VMRuntimeDir(profile, workload string) (string, error) {
	dir, err := profileRunDir(profile)
	if err != nil {
		return "", err
	}
	if err := ValidateName("workload name", workload); err != nil {
		return "", err
	}
	return filepath.Join(dir, "vm", workload), nil
}

// VMVsockDir returns the directory holding a microVM's vsock socket.
//
// It is a directory of its own rather than the runtime directory itself
// because it is the only part of a machine that is bound into another
// workload's sandbox. A console attaching to the machine is given this
// and nothing else.
func VMVsockDir(profile, workload string) (string, error) {
	dir, err := VMRuntimeDir(profile, workload)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "vsock"), nil
}

// VMVsockSocket returns the host path of a microVM's vsock socket.
//
// Everything a guest is reached on crosses here. Firecracker multiplexes
// every guest port over this one file, so who may open it is the whole of
// the access control on a machine's supervisor and on the consoles
// attached to it.
func VMVsockSocket(profile, workload string) (string, error) {
	dir, err := VMVsockDir(profile, workload)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, vmVsockSocketName), nil
}

// VMAPISocket returns the host path of a microVM's firecracker API
// socket.
//
// It is deliberately not under VMVsockDir, and the two must not be tidied
// together. The API socket takes requests to attach drives, to read and
// write guest memory and to stop the machine, so whoever can open it owns
// the VM and everything in it. VMVsockDir is bound into the sandbox of
// every workload that attaches a console, which is an ordinary workload
// running an ordinary terminal. Keeping the API socket one level up is
// what makes that bind safe to give away.
func VMAPISocket(profile, workload string) (string, error) {
	dir, err := VMRuntimeDir(profile, workload)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, vmAPISocketName), nil
}

// InVMConsoleSocket returns the path VMVsockSocket has inside the sandbox
// of a workload that attaches to the machine.
//
// It is not under InWorkloadAgentDir, because that directory holds the
// attaching workload's own supervisor socket and the two are different
// machines answering different protocols.
func InVMConsoleSocket() string {
	return filepath.Join("/run/qubesome-vm", vmVsockSocketName)
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
	dir, err := JoinProfilePath(root, path)
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
