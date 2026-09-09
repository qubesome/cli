package images

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qubesome/cli/internal/files"
)

// refNameAnnotation is where an OCI layout records the tag of a manifest.
const refNameAnnotation = "org.opencontainers.image.ref.name"

// Store is an OCI image store on disk.
//
// Every image reference gets its own OCI layout, and each is unpacked once
// per manifest digest, so profiles sharing an image share one extracted
// root filesystem and a restart costs no extraction at all.
type Store struct {
	// Root is the directory holding the oci layouts and the unpacked
	// bundles.
	Root string

	// cmdRunner is a test seam for Pull and Unpack. Production shells out
	// to skopeo and umoci through runCmd, and tests substitute a fake so
	// the pull, unpack, reuse and error paths can be verified without
	// either binary installed.
	cmdRunner func(bin string, args []string) error
}

// run invokes cmdRunner if a test has set one, and runCmd otherwise.
func (s *Store) run(bin string, args []string) error {
	if s.cmdRunner != nil {
		return s.cmdRunner(bin, args)
	}
	return runCmd(bin, args)
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

// layout returns the OCI layout directory holding ref.
//
// Every reference gets a layout of its own. A shared layout has each pull
// rewrite the same index.json, and skopeo's read-modify-write of it is
// unlocked, so two pulls at once can lose one of the entries. Separate
// layouts remove that between distinct images, and they also mean a
// reference is looked up in a directory that holds nothing else.
//
// The key is a generated single path component, but it is still checked
// before it names a directory, as bundleDir's is.
func (s *Store) layout(ref string) (string, error) {
	key := storeKey(ref)
	if err := files.ValidateName("image store key", key); err != nil {
		return "", err
	}

	dir := filepath.Join(s.Root, "oci", key)

	// Both tools cut the layout from the key at the FIRST colon. umoci
	// does it on the whole --image, and skopeo on what is left once the
	// oci: transport prefix has been taken off the front the same way. So
	// a colon anywhere in this path would be read as the separator, and
	// the tool would work on some other directory under some other tag.
	// storeKey never emits one, but the root is derived from the home
	// directory, which is not qubesome's to constrain. Refuse it here,
	// where both tools get their path, rather than let one of them fail on
	// a path it has already misread.
	if strings.ContainsRune(dir, ':') {
		return "", fmt.Errorf("image store path %q contains a colon, and skopeo and umoci both "+
			"read the first colon as the end of the path: move the qubesome directory to a path "+
			"without one", dir)
	}

	return dir, nil
}

// Digest resolves a reference to the manifest digest recorded in the
// layout's index.
func (s *Store) Digest(ref string) (string, error) {
	layout, err := s.layout(ref)
	if err != nil {
		return "", fmt.Errorf("failed to resolve layout for %q: %w", ref, err)
	}

	path := filepath.Join(layout, "index.json")

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

	want := storeKey(ref)
	for _, m := range index.Manifests {
		if m.Annotations[refNameAnnotation] == want {
			return m.Digest, nil
		}
	}

	return "", fmt.Errorf("image %q is not in the store: no manifest named %q", ref, want)
}

// Resolve returns the bundle for an image the store already holds, without
// reaching the network.
//
// It is what lets a profile start offline: the image is pulled only when
// this fails. A bundle is renamed into place complete, so a readable
// config.json means the unpack finished.
func (s *Store) Resolve(ref string) (Bundle, error) {
	digest, err := s.Digest(ref)
	if err != nil {
		return Bundle{}, err
	}

	dir, err := s.bundleDir(digest)
	if err != nil {
		return Bundle{}, fmt.Errorf("failed to resolve bundle dir for %q: %w", ref, err)
	}

	return readBundle(dir)
}

// bundleDir returns the unpack destination for a digest. The digest is
// read out of a layout's index.json rather than typed by a user, but the
// file it is read from is on disk and it names a directory, so it is
// checked as a single path component before it does.
func (s *Store) bundleDir(digest string) (string, error) {
	name := strings.ReplaceAll(digest, ":", "-")
	if err := files.ValidateName("image digest", name); err != nil {
		return "", err
	}

	return filepath.Join(s.Root, "unpacked", name), nil
}

const (
	// keyReadableMax bounds the readable half of a store key, so a long
	// reference cannot push the key past a filename length limit.
	keyReadableMax = 64

	// keyDigestBytes is how much of the reference digest the key carries.
	// Sixty four bits is what makes the mapping injective in practice,
	// and the key is not a security boundary: an attacker who picks the
	// references also picks which image each one names.
	keyDigestBytes = 8
)

// storeKey returns the key an image reference is stored under.
//
// It names both the layout directory and the reference within it, and the
// whole reference goes into it. Keying by tag alone made
// ghcr.io/qubesome/xorg:latest and ghcr.io/qubesome/kali:latest share one
// entry in one layout, so whichever was pulled last owned it and a profile
// could silently run the other image's root filesystem.
//
// The alphabet is what both tools accept as the tag trailing the layout
// path, the one part of their image arguments that is spelled alike.
// skopeo matches a ref against [A-Za-z0-9._-]+, and umoci applies the OCI
// ref name grammar, which wants alphanumerics at both ends and single
// separators between them. So only alphanumerics survive from the
// reference, a run of anything else becomes a single hyphen, and a digest
// of the whole reference is appended to keep distinct references distinct.
// A colon never survives, which is half of what keeps the key
// unambiguous. The other half is the layout path, which layout checks,
// because umoci cuts at the first colon and the path comes ahead of the
// key.
func storeKey(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	digest := hex.EncodeToString(sum[:keyDigestBytes])

	var b strings.Builder
	sep := false

	for i := 0; i < len(ref); i++ {
		c := ref[i]
		if !alphanumeric(c) {
			sep = true
			continue
		}

		// A separator is only emitted before the next alphanumeric, so a
		// key never starts or ends with one and never carries two in a
		// row. It counts towards the bound with the byte it precedes, so
		// the two are written together or not at all.
		need := 1
		if sep && b.Len() > 0 {
			need = 2
		}
		if b.Len()+need > keyReadableMax {
			break
		}

		if need == 2 {
			b.WriteByte('-')
		}
		sep = false

		b.WriteByte(c)
	}

	if b.Len() == 0 {
		return digest
	}

	return b.String() + "-" + digest
}

func alphanumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
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
