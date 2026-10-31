package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qubesome/cli/internal/types"
)

// bindIdentity prevents two configurations using the same session gateway
// from assigning an ambiguous combined name to different workload/profile
// pairs. Bindings survive restarts until the runtime directory is removed.
func (g Gateway) bindIdentity(ew types.EffectiveWorkload) error {
	if ew.Profile == nil || ew.Name != ew.Workload.Name+"-"+ew.Profile.Name {
		return errors.New("invalid gateway workload identity")
	}
	lock, err := acquire(g.LockPath)
	if err != nil {
		return err
	}
	defer lock.Close()
	path := filepath.Join(g.Dir, "gateway-identities.json")
	bindings := make(map[string][2]string)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(data, &bindings); err != nil {
			return fmt.Errorf("read gateway identities: %w", err)
		}
		if bindings == nil {
			return errors.New("invalid gateway identity record")
		}
	}
	pair := [2]string{ew.Profile.Name, ew.Workload.Name}
	if previous, ok := bindings[ew.Name]; ok {
		if previous != pair {
			return fmt.Errorf("gateway name %q is already bound to workload %q in profile %q", ew.Name, previous[1], previous[0])
		}
		return nil
	}
	bindings[ew.Name] = pair
	data, err = json.Marshal(bindings)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(g.Dir, ".gateway-identities-*")
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
	return os.Rename(f.Name(), path)
}
