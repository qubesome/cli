package images

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
)

// Pull copies an image into the store. skopeo verifies the manifest and
// every blob digest, and nothing is unpacked if that fails.
func (s *Store) Pull(ref string) error {
	if err := os.MkdirAll(s.layout(), files.DirMode); err != nil {
		return fmt.Errorf("failed to create image store %q: %w", s.layout(), err)
	}

	slog.Info("pulling container image", "image", ref)

	return s.run(files.SkopeoBinary, s.pullArgs(ref))
}

func (s *Store) pullArgs(ref string) []string {
	return []string{
		"copy",
		"docker://" + ref,
		"oci:" + s.layout() + ":" + tag(ref),
	}
}

// Unpack extracts an image and returns its bundle.
//
// Extraction is keyed by manifest digest, so an image already unpacked is
// reused as it is. The bundle is built under a temporary name and renamed
// into place, so an interrupted unpack is never visible under the name a
// later start looks for.
func (s *Store) Unpack(ref string) (Bundle, error) {
	digest, err := s.Digest(ref)
	if err != nil {
		return Bundle{}, err
	}

	dir, err := s.bundleDir(digest)
	if err != nil {
		return Bundle{}, fmt.Errorf("failed to resolve bundle dir for %q: %w", ref, err)
	}

	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return readBundle(dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Bundle{}, fmt.Errorf("failed to stat bundle %q: %w", dir, err)
	}

	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, files.DirMode); err != nil {
		return Bundle{}, fmt.Errorf("failed to create bundle dir %q: %w", parent, err)
	}

	tmp, err := os.MkdirTemp(parent, "tmp-")
	if err != nil {
		return Bundle{}, fmt.Errorf("failed to create temp bundle dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	// umoci creates the bundle itself and refuses an existing directory.
	target := filepath.Join(tmp, "bundle")

	slog.Info("unpacking container image", "image", ref, "digest", digest)
	if err := s.run(files.UmociBinary, s.unpackArgs(ref, target)); err != nil {
		return Bundle{}, err
	}

	if err := os.Rename(target, dir); err != nil {
		return Bundle{}, fmt.Errorf("failed to move bundle into place: %w", err)
	}

	return readBundle(dir)
}

func (s *Store) unpackArgs(ref, dest string) []string {
	return []string{
		"unpack",
		"--rootless",
		"--image", "oci:" + s.layout() + ":" + tag(ref),
		dest,
	}
}

// runCmd executes a helper and attaches its stderr to any failure,
// following the pattern established in
// internal/runners/util/container/home.go. The reason a pull or an unpack
// failed is always on stderr.
func runCmd(bin string, args []string) error {
	slog.Debug("exec", "binary", bin, "args", args)

	cmd := execabs.Command(bin, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
			return fmt.Errorf("%s %v: %w: %s", filepath.Base(bin), args, err, msg)
		}
		return fmt.Errorf("%s %v: %w", filepath.Base(bin), args, err)
	}

	return nil
}
