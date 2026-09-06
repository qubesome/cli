package images

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/qubesome/cli/internal/files"
)

// refNameAnnotation is where an OCI layout records the tag of a manifest.
const refNameAnnotation = "org.opencontainers.image.ref.name"

// Store is an OCI image store on disk.
//
// Images are pulled into a shared OCI layout and unpacked once per manifest
// digest, so profiles sharing an image share one extracted root filesystem
// and a restart costs no extraction at all.
type Store struct {
	// Root is the directory holding the oci layout and the unpacked
	// bundles.
	Root string
}

// NewStore returns the store under the qubesome directory.
func NewStore() *Store {
	return &Store{Root: filepath.Join(files.QubesomeDir(), "images")}
}

// Bundle describes an unpacked image.
type Bundle struct {
	// Rootfs is the extracted root filesystem, used read-only as the lower
	// layer of the sandbox overlay.
	Rootfs string
	UID    int
	GID    int
	// Env is the image environment. Container runners applied this
	// implicitly and bwrap does not, so the caller must pass it on.
	Env []string
	Cwd string
}

func (s *Store) layout() string {
	return filepath.Join(s.Root, "oci")
}

// Digest resolves a reference to the manifest digest recorded in the
// layout's index.
func (s *Store) Digest(ref string) (string, error) {
	path := filepath.Join(s.layout(), "index.json")

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read image index %q: %w", path, err)
	}

	var index struct {
		Manifests []struct {
			Digest      string            `json:"digest"`
			Annotations map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return "", fmt.Errorf("failed to parse image index %q: %w", path, err)
	}

	want := tag(ref)
	for _, m := range index.Manifests {
		if m.Annotations[refNameAnnotation] == want {
			return m.Digest, nil
		}
	}

	return "", fmt.Errorf("image %q is not in the store: no manifest tagged %q", ref, want)
}

// bundleDir returns the unpack destination for a digest. The digest comes
// from the index rather than from user input, but it still names a
// directory, so it is joined securely.
func (s *Store) bundleDir(digest string) (string, error) {
	return securejoin.SecureJoin(filepath.Join(s.Root, "unpacked"),
		strings.ReplaceAll(digest, ":", "-"))
}

// tag returns the tag of a reference, defaulting to latest.
//
// A colon in the final path element separates the tag. A colon earlier in
// the reference is a registry port, which is why only the last element is
// considered.
//
// A digest reference such as name@sha256:abc is not handled specially. The
// substring after the first colon in the last path element is returned as
// if it were a tag, which is wrong for a digest reference. Every caller
// today only passes tagged references, so this is left unhandled rather
// than guessed at.
func tag(ref string) string {
	last := ref
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		last = ref[i+1:]
	}
	if _, t, ok := strings.Cut(last, ":"); ok {
		return t
	}
	return "latest"
}

// readBundle reads the OCI runtime spec umoci generated for a bundle.
//
// The uid is taken from here rather than resolved by qubesome, because
// umoci already resolved the image's user name against the image's own
// /etc/passwd during unpack.
func readBundle(dir string) (Bundle, error) {
	path := filepath.Join(dir, "config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, fmt.Errorf("failed to read bundle config %q: %w", path, err)
	}

	var cfg struct {
		Process struct {
			User struct {
				UID int `json:"uid"`
				GID int `json:"gid"`
			} `json:"user"`
			Cwd string   `json:"cwd"`
			Env []string `json:"env"`
		} `json:"process"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Bundle{}, fmt.Errorf("failed to parse bundle config %q: %w", path, err)
	}

	return Bundle{
		Rootfs: filepath.Join(dir, "rootfs"),
		UID:    cfg.Process.User.UID,
		GID:    cfg.Process.User.GID,
		Env:    cfg.Process.Env,
		Cwd:    cfg.Process.Cwd,
	}, nil
}
