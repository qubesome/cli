package images

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/qubesome/cli/internal/files"
)

const (
	// keepVersions is how many versions of each reference survive a prune,
	// the current one and the one before it. A profile started before a
	// refresh is still running on the previous version, and its sandbox
	// uses that bundle as its root filesystem.
	keepVersions = 2

	// staleTempAge is how old a temporary unpack directory must be before
	// a prune treats it as left behind by a killed unpack. A younger one
	// may belong to an unpack that is still running.
	staleTempAge = time.Hour
)

// Prune removes the image versions that fell out of the store's window.
//
// It runs on the default store, the same one a refresh fills.
func Prune() error {
	return NewStore().Prune()
}

// Prune keeps the last keepVersions versions of every reference in the
// store and removes every other unpacked bundle. It then has umoci drop
// the blobs no layout index still references.
//
// What survives is decided across all references at once, because two
// references can resolve to one digest and share its bundle. Images the
// configuration no longer names are kept like any other, so a prune never
// needs to know which configuration is in use.
func (s *Store) Prune() error {
	keep, layouts, err := s.retained()
	if err != nil {
		return err
	}

	if err := s.pruneBundles(keep); err != nil {
		return err
	}

	for _, layout := range layouts {
		slog.Debug("collecting unreferenced blobs", "layout", layout)
		if err := s.run(files.UmociBinary, gcArgs(layout)); err != nil {
			slog.Error("cannot collect unreferenced blobs", "layout", layout, "error", err)
		}
	}

	return nil
}

func gcArgs(layout string) []string {
	return []string{"gc", "--layout", layout}
}

// retained returns the bundle directory names every layout still needs,
// and trims each history down to the versions it kept.
func (s *Store) retained() (map[string]struct{}, []string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Root, "oci"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list image layouts: %w", err)
	}

	keep := map[string]struct{}{}
	layouts := make([]string, 0, len(entries))

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		key := e.Name()
		if err := files.ValidateName("image store key", key); err != nil {
			return nil, nil, err
		}

		layout := filepath.Join(s.Root, "oci", key)
		current, err := currentDigest(layout, key)
		if err != nil {
			return nil, nil, err
		}

		hist, err := s.history(key)
		if err != nil {
			return nil, nil, err
		}

		versions := lastVersions(appendVersion(hist, current), keepVersions)
		for _, d := range versions {
			name, err := s.bundleName(d)
			if err != nil {
				return nil, nil, err
			}
			keep[name] = struct{}{}
		}

		if !slices.Equal(versions, hist) {
			if err := s.writeHistory(key, versions); err != nil {
				return nil, nil, err
			}
		}

		if current != "" {
			layouts = append(layouts, layout)
		}
	}

	return keep, layouts, nil
}

// pruneBundles removes every unpacked bundle outside keep, and every
// temporary unpack directory old enough to have been abandoned.
func (s *Store) pruneBundles(keep map[string]struct{}) error {
	dir := filepath.Join(s.Root, "unpacked")

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot list unpacked images: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if _, ok := keep[name]; ok {
			continue
		}

		if strings.HasPrefix(name, "tmp-") {
			fi, err := e.Info()
			if err != nil {
				return fmt.Errorf("cannot stat %q: %w", name, err)
			}
			if time.Since(fi.ModTime()) < staleTempAge {
				continue
			}
		}

		path := filepath.Join(dir, name)
		slog.Info("removing image version", "path", path)
		if err := removeAll(path); err != nil {
			return fmt.Errorf("cannot remove %q: %w", path, err)
		}
	}

	return nil
}

// removeAll is os.RemoveAll for a tree that may hold directories without
// the owner write bit. A rootless unpack keeps the modes an image ships,
// and a directory the owner cannot write cannot have its entries removed.
//
// The tree comes from an image, so the walk is scoped to an os.Root and
// cannot be led out of it by a symlink.
func removeAll(path string) error {
	root, err := os.OpenRoot(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		return root.Chmod(p, files.DirMode)
	})
	root.Close()
	if err != nil {
		return err
	}

	return os.RemoveAll(path)
}

// record appends ref's current digest to its history, after a pull.
//
// The index only ever names the version a pull left, so the history is
// the one place the versions before it are known.
func (s *Store) record(ref string) error {
	key := storeKey(ref)

	layout, err := s.layout(ref)
	if err != nil {
		return fmt.Errorf("failed to resolve layout for %q: %w", ref, err)
	}

	current, err := currentDigest(layout, key)
	if err != nil {
		return err
	}
	if current == "" {
		return fmt.Errorf("image %q is not in the store: no manifest named %q", ref, key)
	}

	hist, err := s.history(key)
	if err != nil {
		return err
	}

	next := appendVersion(hist, current)
	if slices.Equal(next, hist) {
		return nil
	}

	return s.writeHistory(key, next)
}

// appendVersion makes digest the latest version in hist. A digest the
// history already holds moves to the end instead of appearing twice. An
// empty digest leaves hist as it is.
func appendVersion(hist []string, digest string) []string {
	if digest == "" {
		return hist
	}
	if len(hist) > 0 && hist[len(hist)-1] == digest {
		return hist
	}

	out := make([]string, 0, len(hist)+1)
	for _, d := range hist {
		if d != digest {
			out = append(out, d)
		}
	}

	return append(out, digest)
}

func lastVersions(hist []string, n int) []string {
	if len(hist) <= n {
		return hist
	}

	return hist[len(hist)-n:]
}

// historyPath returns the file holding the versions recorded for key.
//
// It lives outside the layout, which is skopeo's and umoci's to manage.
func (s *Store) historyPath(key string) string {
	return filepath.Join(s.Root, "history", key)
}

// history returns the digests recorded for key, oldest first.
func (s *Store) history(key string) ([]string, error) {
	path := s.historyPath(key)

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read image history %q: %w", path, err)
	}

	var hist []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			hist = append(hist, line)
		}
	}

	return hist, sc.Err()
}

// writeHistory replaces the history for key. It writes to a temporary
// file and renames it into place, so a concurrent reader sees either the
// old history or the new one and never half of either.
func (s *Store) writeHistory(key string, hist []string) error {
	path := s.historyPath(key)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, files.DirMode); err != nil {
		return fmt.Errorf("cannot create image history dir %q: %w", dir, err)
	}

	f, err := os.CreateTemp(dir, "tmp-")
	if err != nil {
		return fmt.Errorf("cannot create image history: %w", err)
	}
	defer os.Remove(f.Name())

	var b strings.Builder
	for _, d := range hist {
		b.WriteString(d)
		b.WriteByte('\n')
	}

	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return fmt.Errorf("cannot write image history: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cannot write image history: %w", err)
	}

	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("cannot replace image history %q: %w", path, err)
	}

	return nil
}
