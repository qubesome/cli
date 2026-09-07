package env

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
)

func init() { //nolint
	h, _ := os.UserHomeDir()
	_ = Update("HOME", h)
}

// mu guards mapping.
//
// A profile serves workload runs from its inception server goroutine
// while its own start expands paths, and each run registers the config
// root and the profile's external drive labels before it expands
// anything. So reads and writes genuinely overlap.
var mu sync.RWMutex

var mapping = map[string]string{
	"HOME":   "",
	"GITDIR": "",
}

func Update(k, v string) error {
	slog.Debug("setting env", k, v)

	mu.Lock()
	defer mu.Unlock()

	if _, ok := mapping[k]; ok {
		mapping[k] = v
		return nil
	}
	return fmt.Errorf("%q is not an expandable env var", k)
}

func Add(k, v string) {
	mu.Lock()
	defer mu.Unlock()

	mapping[k] = v
}

func Expand(in string) string {
	return os.Expand(in, expand)
}

func expand(s string) string {
	mu.RLock()
	defer mu.RUnlock()

	if out, ok := mapping[s]; ok {
		return out
	}
	return ""
}
