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
	layout, err := s.layout(ref)
	if err != nil {
		return fmt.Errorf("failed to resolve layout for %q: %w", ref, err)
	}

	if err := os.MkdirAll(layout, files.DirMode); err != nil {
		return fmt.Errorf("failed to create image store %q: %w", layout, err)
	}

	slog.Info("pulling container image", "image", ref)

	return s.run(files.SkopeoBinary, pullArgs(layout, ref))
}

func pullArgs(layout, ref string) []string {
	return []string{
		"copy",
		"docker://" + ref,
		skopeoImage(layout, ref),
	}
}

// skopeoImage and umociImage build the same layout and key for the two
// tools, which do not share a syntax for it.
//
// skopeo names a destination by transport, so it wants the oci: prefix.
// umoci takes a bare path[:tag] and cuts it at the FIRST colon, so the
// same prefix makes it read "oci" as the directory and everything after it
// as the tag, which it then rejects as an invalid reference name. Keep the
// two forms apart. Folding them back into one breaks whichever tool loses.
//
// The prefix is the whole of the difference. Once it is off, both tools cut
// the layout from the key at the first colon, which is why a layout path
// carrying one is refused when the layout is resolved.
func skopeoImage(layout, ref string) string {
	return "oci:" + umociImage(layout, ref)
}

func umociImage(layout, ref string) string {
	return layout + ":" + storeKey(ref)
}

// Unpack extracts an image and returns its bundle.
//
// Extraction is keyed by manifest digest, so an image already unpacked is
// reused as it is. The bundle is built under a temporary name and renamed
// into place, so an interrupted unpack is never visible under the name a
// later start looks for.
//
// Two starts can unpack the same image at once, and profiles do share
// images, so this happens in practice. Both extract, then both rename onto
// the same digest directory. os.Rename refuses an existing directory, so
// the second rename fails rather than replacing anything. The loser reuses
// what the winner put there instead of failing the start.
func (s *Store) Unpack(ref string) (Bundle, error) {
	layout, err := s.layout(ref)
	if err != nil {
		return Bundle{}, fmt.Errorf("failed to resolve layout for %q: %w", ref, err)
	}

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
	if err := s.run(files.UmociBinary, unpackArgs(layout, ref, target)); err != nil {
		return Bundle{}, err
	}

	if err := os.Rename(target, dir); err != nil {
		// A concurrent unpack of the same image is the expected reason to
		// land here, and it leaves a complete bundle behind, so reuse it.
		// Any other rename failure has no bundle to fall back on.
		if _, statErr := os.Stat(filepath.Join(dir, "config.json")); statErr == nil {
			slog.Debug("bundle already unpacked by a concurrent start", "digest", digest)
			return readBundle(dir)
		}

		return Bundle{}, fmt.Errorf("failed to move bundle into place: %w", err)
	}

	return readBundle(dir)
}

func unpackArgs(layout, ref, dest string) []string {
	return []string{
		"unpack",
		"--rootless",
		"--image", umociImage(layout, ref),
		dest,
	}
}

// runCmd executes a helper and attaches its stderr to any failure. The
// reason a pull or an unpack failed is always on stderr.
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
