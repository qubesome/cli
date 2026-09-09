package images

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"go.yaml.in/yaml/v3"
)

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	slog.Debug("images.Run", "options", o)
	return PullAll(o.Config)
}

// RefreshExpired re-pulls every image in a config once the last check is
// older than refreshExpiration. It blocks, so it belongs on a goroutine of a
// process that outlives it.
//
// It is deliberately not called from a workload launch. It used to be,
// with the launch waiting on it, so opening one app re-pulled and
// re-unpacked every image the configuration named and the caller waited
// for all of them. The refresh now runs where a profile is being started,
// which is a process that stays up and where the wait is expected
// anyway.
func RefreshExpired(cfg *types.Config) {
	refreshExpired(NewStore(), cfg)
}

func refreshExpired(s *Store, cfg *types.Config) {
	if cfg.WorkloadPullMode != types.Background {
		return
	}

	exp, err := pullExpired()
	if err != nil {
		slog.Error("cannot tell whether images are due a refresh", "error", err)
		return
	}
	if !exp {
		return
	}

	if err := pullAll(s, cfg); err != nil {
		slog.Error("error pulling images", "error", err)
	}
}

var (
	// refreshExpiration is how stale the store may get before starting a
	// profile refreshes it in the background. Three days rather than one
	// because a refresh re-fetches every image the configuration names,
	// which is minutes of network and disk, and because nothing about an
	// image qubesome runs changes daily.
	refreshExpiration = 72 * time.Hour
)

func pullExpired() (bool, error) {
	fn := files.ImagesLastCheckedPath()
	fi, err := os.Stat(fn)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("cannot stat %q: %w", fn, err)
		}
		if err := os.WriteFile(fn, []byte{}, files.FileMode); err != nil {
			return false, fmt.Errorf("cannot create file %q: %w", fn, err)
		}
		_, err = os.Stat(fn)
		if err != nil {
			return false, fmt.Errorf("cannot stat %q post-creation: %w", fn, err)
		}
		return true, nil
	}

	if fi.ModTime().Before(time.Now().Add(-refreshExpiration)) {
		if err := os.WriteFile(fn, []byte{}, files.FileMode); err != nil {
			return false, fmt.Errorf("cannot update file %q: %w", fn, err)
		}
		return true, nil
	}

	return false, nil
}

// PreemptWorkloadImages pulls what the store lacks on the first run, so
// that opening an app later does not wait on a pull.
//
// The profile image is in the store by the time this runs, so what it
// fetches is the workload images.
func PreemptWorkloadImages(cfg *types.Config) {
	slog.Info("preemptively pulling workload images, which happens once and saves waiting on the first launch of each")

	_ = pullMissing(NewStore(), cfg)
}

// FirstRun reports whether this host has never checked its images.
//
// It answers from the same sentinel RefreshExpired keeps its timestamp
// in, because the two questions have one answer: a host that has never
// refreshed is a host that has never been offered a preload either.
//
// The offer used to be made whenever an image was missing, and the
// sentinel was only consulted afterwards, inside the pull it led to. So
// declining left nothing recorded and the question came back on every
// start, which is the one answer that made it permanent.
func FirstRun() bool {
	_, err := os.Stat(files.ImagesLastCheckedPath())

	return errors.Is(err, os.ErrNotExist)
}

// PullAll refreshes every image in a config.
//
// Images are pulled whether or not the store already holds them, because
// refreshing is the whole point of the command behind this.
func PullAll(cfg *types.Config) error {
	return pullAll(NewStore(), cfg)
}

func pullAll(s *Store, cfg *types.Config) error {
	imgs, err := ConfigImages(cfg)
	if err != nil {
		return fmt.Errorf("cannot get images: %w", err)
	}

	for _, img := range imgs {
		if _, err := refreshImage(s, img); err != nil {
			slog.Error("cannot pull image", "image", img, "error", err)
		}
	}

	return nil
}

// pullMissing pulls only the images the store cannot already provide.
func pullMissing(s *Store, cfg *types.Config) error {
	imgs, err := ConfigImages(cfg)
	if err != nil {
		return fmt.Errorf("cannot get images: %w", err)
	}

	for _, img := range imgs {
		if _, err := pullImage(s, img); err != nil {
			slog.Error("cannot pull image", "image", img, "error", err)
		}
	}

	return nil
}

// PullProfileImage returns the bundle for a profile image, pulling it into
// the OCI store only when the store cannot already provide it.
func PullProfileImage(ref string) (Bundle, error) {
	return pullImage(NewStore(), ref)
}

// pullImage skips the pull for an image the store already holds.
//
// A warm store lets a profile start with no network, and it is what keeps
// a repeated workload launch off the registry. Refreshing is PullAll's
// job, not a side effect of starting something.
func pullImage(s *Store, ref string) (Bundle, error) {
	if b, err := s.Resolve(ref); err == nil {
		slog.Debug("image is already in the store", "image", ref)
		return b, nil
	}

	return refreshImage(s, ref)
}

// refreshImage pulls and unpacks ref whether or not the store already
// holds it.
func refreshImage(s *Store, ref string) (Bundle, error) {
	if err := s.Pull(ref); err != nil {
		return Bundle{}, err
	}

	return s.Unpack(ref)
}

// HasImage reports whether the store can already provide ref.
//
// It is the single reference form of MissingImages, for a caller that
// holds one image and no config.
func HasImage(ref string) bool {
	return hasImage(NewStore(), ref)
}

func hasImage(s *Store, ref string) bool {
	_, err := s.Resolve(ref)

	return err == nil
}

// MissingImages returns the config images the store cannot resolve.
func MissingImages(cfg *types.Config) ([]string, error) {
	return missingImages(NewStore(), cfg)
}

func missingImages(s *Store, cfg *types.Config) ([]string, error) {
	imgs, err := ConfigImages(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot get images: %w", err)
	}

	missing := make([]string, 0, len(imgs))
	for _, img := range imgs {
		if hasImage(s, img) {
			continue
		}

		missing = append(missing, img)
	}

	return missing, nil
}

// ConfigImages returns every unique image a config references, profile and
// workload alike.
//
// Profile and workload images were listed apart while each half lived in a
// store of its own. One store holds them all, so one list answers for
// every image.
func ConfigImages(cfg *types.Config) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	seen := map[string]struct{}{}
	imgs := make([]string, 0, len(cfg.Profiles))

	add := func(img string) {
		if img == "" {
			return
		}
		if _, ok := seen[img]; ok {
			return
		}
		seen[img] = struct{}{}
		imgs = append(imgs, img)
	}

	for _, p := range cfg.Profiles {
		add(p.Image)
	}

	// The gateway's image is one the configuration names, so it belongs
	// in the same list as every other. It was missing, which made
	// refresh fetch everything except the one image a workload with a
	// gateway network cannot start without, and left MissingImages
	// reporting a complete store that was not.
	if cfg.Gateway != nil {
		add(cfg.Gateway.Image)
	}

	wf, err := cfg.WorkloadFiles()
	if err != nil {
		return nil, fmt.Errorf("cannot get workloads files: %w", err)
	}

	if len(wf) == 0 {
		fmt.Println("no workloads found")
	}

	for _, fn := range wf {
		fi, err := os.Stat(fn)
		if err != nil {
			return nil, fmt.Errorf("cannot stat file %q: %w", fn, err)
		}

		if !fi.Mode().IsRegular() {
			continue
		}

		data, err := os.ReadFile(fn)
		if err != nil {
			return nil, fmt.Errorf("cannot read file %q: %w", fn, err)
		}

		w := types.Workload{}
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true) // Enforces that all YAML fields match struct fields exactly.
		if err := decoder.Decode(&w); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("workload file %q is empty", fn)
			}
			return nil, fmt.Errorf("cannot unmarshal workload file %q: %w", fn, err)
		}

		add(w.Image)
	}

	return imgs, nil
}
