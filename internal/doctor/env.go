package doctor

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/execabs"
)

// Env is the host as doctor sees it.
//
// It exists so that the checks can be tested without a container runner, an
// X server or a GPU. Every check reads the host through this and nothing
// else, so a fake is enough to drive any of them.
type Env interface {
	// LookPath reports where a binary is, or an error if it is absent.
	LookPath(file string) (string, error)

	// Stat reports on a path.
	Stat(path string) (os.FileInfo, error)

	// Getenv reads an environment variable.
	Getenv(key string) string

	// Output runs a command and returns its combined output. The error
	// says whether it failed, and the output usually says why, so both
	// are reported together.
	Output(name string, args ...string) ([]byte, error)
}

// OSEnv is the real host.
type OSEnv struct {
	// Timeout bounds each command doctor runs. A container runner whose
	// daemon is unreachable often hangs rather than failing, and doctor
	// exists to report that rather than to hang alongside it.
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

func (e *OSEnv) Getenv(key string) string {
	return os.Getenv(key)
}

func (e *OSEnv) Output(name string, args ...string) ([]byte, error) {
	ctx, cancel := contextWithTimeout(e.Timeout)
	defer cancel()

	cmd := execabs.CommandContext(ctx, name, args...)

	return cmd.CombinedOutput()
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}

	return context.WithTimeout(context.Background(), d)
}
