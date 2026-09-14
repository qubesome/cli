package gateway

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/keyring/backend"
	"github.com/qubesome/cli/pkg/control"
	"go.yaml.in/yaml/v3"
)

// keyringValues reads only references in inject rules. The gateway remains
// responsible for validating the complete policy. Values never go to disk or
// into the sandbox environment. They travel on the mTLS control channel.
func keyringValues(data []byte, get func(string, string) (string, error)) (map[string]string, error) {
	var policy struct {
		Workloads map[string]struct {
			Inject []struct {
				ValueFrom struct {
					File    string                    `yaml:"file"`
					Env     string                    `yaml:"env"`
					Keyring *control.KeyringReference `yaml:"keyring"`
				} `yaml:"valueFrom"`
			} `yaml:"inject"`
		} `yaml:"workloads"`
	}
	if err := yaml.Unmarshal(data, &policy); err != nil {
		return nil, errors.New("cannot parse gateway policy for keyring references")
	}
	values := make(map[string]string)
	for name, workload := range policy.Workloads {
		for i, rule := range workload.Inject {
			ref := rule.ValueFrom.Keyring
			if ref == nil {
				continue
			}
			if ref.Service == "" || ref.Account == "" || rule.ValueFrom.File != "" || rule.ValueFrom.Env != "" {
				return nil, fmt.Errorf("workload %q inject[%d]: keyring requires service and account and cannot be combined with file or env", name, i)
			}
			id := control.KeyringID(ref.Service, ref.Account)
			if _, ok := values[id]; ok {
				continue
			}
			value, err := get(ref.Service, ref.Account)
			if err != nil {
				// Backend errors may contain sensitive data. Do not wrap them.
				return nil, fmt.Errorf("workload %q inject[%d]: cannot read host keyring entry (missing, locked, or unavailable)", name, i)
			}
			if value == "" || strings.ContainsAny(value, "\r\n\x00") {
				return nil, fmt.Errorf("workload %q inject[%d]: keyring entry is empty or contains invalid header characters", name, i)
			}
			values[id] = value
		}
	}
	return values, nil
}

func (g Gateway) policySourcePath() string { return filepath.Join(g.Dir, "gateway-policy-source") }
func (g Gateway) policyDir() string        { return filepath.Join(g.Dir, "gateway-policy") }

// Refresh reloads the policy and host-store values without restarting workloads.
func (g Gateway) Refresh() error {
	if _, err := g.SandboxPID(); err != nil {
		return err
	}
	if err := g.ready(); err != nil {
		return err
	}
	return g.reload(false)
}

// stagePolicy copies only configuration, never resolved values. Mounting its
// directory lets atomic replacements become visible to a running gateway.
func (g Gateway) stagePolicy(data []byte) error {
	if err := os.MkdirAll(g.policyDir(), files.DirMode); err != nil {
		return err
	}
	f, err := os.CreateTemp(g.policyDir(), ".policy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(g.policyDir(), "gateway.yml"))
}

// reloadKeyring stages the policy the gateway was started from and delivers
// the host keyring values it names, and reports whether it delivered any.
//
// It is always the original policy, never the configuration of an unrelated
// launch reusing this session's gateway. The caller holds the gateway lock
// across the staging and the call, so concurrent launches cannot mix one
// policy with another's credentials.
//
// A policy that names no keyring values is staged and nothing more. The
// caller then asks the gateway to re-read it the way it always has, which
// is what keeps a gateway too old to serve Reload working as it did.
func (g Gateway) reloadKeyring(c *Client) (bool, error) {
	source, err := os.ReadFile(g.policySourcePath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// The source record is written only by the trusted launcher in its
	// private session directory. The policy itself may live anywhere.
	data, err := os.ReadFile(string(source)) //nolint:gosec // trusted host configuration path, not workload input.
	if err != nil {
		return false, fmt.Errorf("read gateway policy: %w", err)
	}

	values, err := keyringValues(data, backend.New().Get)
	if err != nil {
		return false, err
	}

	if err := g.stagePolicy(data); err != nil {
		return false, err
	}

	if len(values) == 0 {
		return false, nil
	}

	digest := sha256.Sum256(data)
	return true, c.ReloadKeyring(values, digest[:])
}
