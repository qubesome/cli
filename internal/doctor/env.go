package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/qubesome/cli/internal/gateway"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/runners/util/usb"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/util/drive"
	"golang.org/x/sys/execabs"
)

// Env is the host as doctor sees it.
//
// It exists so that the checks can be tested without a sandbox, an X
// server or a GPU. Every check reads the host through this and nothing
// else, so a fake is enough to drive any of them.
type Env interface {
	// LookPath reports where a binary is, or an error if it is absent.
	LookPath(file string) (string, error)

	// Stat reports on a path.
	Stat(path string) (os.FileInfo, error)

	// Lstat reports on a path without following a final symlink, so that
	// a link is judged as itself rather than as whatever it points at.
	Lstat(path string) (os.FileInfo, error)

	// Readlink returns the target of a symlink. A running profile is
	// reached through one, so this is how doctor gets back to the
	// directory a config was really sourced from.
	Readlink(path string) (string, error)

	// Getenv reads an environment variable.
	Getenv(key string) string

	// UID is the caller's numeric user id, as a string. The session bus
	// lives under /run/user/<uid>, and the dbus client qubesome uses
	// builds that path from the uid rather than from XDG_RUNTIME_DIR, so
	// doctor has to look where the client will.
	UID() string

	// Output runs a command and returns its combined output. The error
	// says whether it failed, and the output usually says why, so both
	// are reported together.
	Output(name string, args ...string) ([]byte, error)

	// Glob reports the paths matching a shell pattern. It is how the
	// checks ask whether a class of device is present at all, since a
	// camera may be video0 or video2 and neither name is fixed.
	Glob(pattern string) ([]string, error)

	// USBNamed resolves the USB device names a workload asks for into the
	// device nodes they refer to, returning nothing for a name that
	// matches no attached device.
	USBNamed(names []string) ([]string, error)

	// Mounted reports whether device is mounted at mount, by reading the
	// kernel's mount table. Statting the mountpoint answers a different
	// question, since the directory a drive mounts over is there whether
	// or not anything is mounted on it.
	Mounted(device, mount string) (bool, error)

	// ImageInStore reports whether the OCI store can already provide an
	// image reference. A profile or a workload starts from an unpacked
	// bundle, so this is what having an image locally now means.
	ImageInStore(ref string) bool

	// SandboxAlive reports whether the sandbox recorded in the state file
	// at path is still running. It is how doctor asks whether a profile
	// is up, since a sandbox has no name to look up and nothing to ask.
	SandboxAlive(path string) bool

	// GatewayReady asks the session's gateway over its control socket
	// whether its resolver, proxy and netfilter ruleset are all up.
	//
	// The question is asked of the gateway rather than of the filesystem.
	// A socket file exists as soon as a listener binds, which is before
	// the ruleset is programmed, so a check that stopped at the file
	// would call a gateway ready while a workload started against it
	// would still have egress with no rules on it.
	GatewayReady() error
}

// OSEnv is the real host.
type OSEnv struct {
	// Timeout bounds each command doctor runs. An X server that has
	// stopped answering hangs the tools that query it rather than failing
	// them, and doctor exists to report that rather than to hang
	// alongside them.
	Timeout time.Duration
}

// NewOSEnv returns an Env backed by the running system.
func NewOSEnv() *OSEnv {
	return &OSEnv{Timeout: 10 * time.Second}
}

func (e *OSEnv) LookPath(file string) (string, error) {
	return execabs.LookPath(file)
}

func (e *OSEnv) Stat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

func (e *OSEnv) Lstat(path string) (os.FileInfo, error) {
	return os.Lstat(path)
}

func (e *OSEnv) Readlink(path string) (string, error) {
	return os.Readlink(path)
}

func (e *OSEnv) Getenv(key string) string {
	return os.Getenv(key)
}

func (e *OSEnv) UID() string {
	return strconv.Itoa(os.Getuid())
}

func (e *OSEnv) Output(name string, args ...string) ([]byte, error) {
	ctx, cancel := contextWithTimeout(e.Timeout)
	defer cancel()

	cmd := execabs.CommandContext(ctx, name, args...)

	return cmd.CombinedOutput()
}

func (e *OSEnv) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

func (e *OSEnv) USBNamed(names []string) ([]string, error) {
	return usb.NamedDevices(names)
}

func (e *OSEnv) Mounted(device, mount string) (bool, error) {
	return drive.Mounts(device, mount)
}

func (e *OSEnv) ImageInStore(ref string) bool {
	return images.HasImage(ref)
}

func (e *OSEnv) SandboxAlive(path string) bool {
	return sandbox.Alive(path)
}

// GatewayReady reaches the running gateway over the control channel the
// launches use, presenting the credentials the launch that started it left
// behind.
//
// Under doctor's own timeout rather than the client's. A launch waits ten
// minutes because behind Ready there may be an image still being pulled,
// and waiting is what it is there to do. doctor is there to report, and a
// gateway that has stopped answering has to become a line in the report
// rather than a command that never returns.
func (e *OSEnv) GatewayReady() error {
	ctx, cancel := contextWithTimeout(e.Timeout)
	defer cancel()

	c, err := gateway.Current().Client()
	if err != nil {
		return err
	}

	return c.Ready(ctx)
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}

	return context.WithTimeout(context.Background(), d)
}
