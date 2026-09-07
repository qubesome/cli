package images

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
)

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	bin := files.ContainerRunnerBinary(o.Runner)

	slog.Debug("images.Run", "options", o)
	return PullAll(bin, o.Config)
}

func Pull(bin string, cfg *types.Config, wg *sync.WaitGroup) error {
	switch cfg.WorkloadPullMode {
	case types.Background:
		wg.Go(func() {
			if exp, _ := pullExpired(); exp {
				err := PullAll(bin, cfg)
				if err != nil {
					slog.Error("error pulling images", "error", err)
				}
			}
		})
	case types.OnDemand:
		// no-op as images will be pull when needed.
	}
	return nil
}

var (
	pullExpiration = 24 * time.Hour
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

	if fi.ModTime().Before(time.Now().Add(-pullExpiration)) {
		if err := os.WriteFile(fn, []byte{}, files.FileMode); err != nil {
			return false, fmt.Errorf("cannot update file %q: %w", fn, err)
		}
		return true, nil
	}

	return false, nil
}

func PreemptWorkloadImages(bin string, cfg *types.Config) {
	slog.Debug("Check need for the preemptive pull of workload images")
	fn := files.ImagesLastCheckedPath()

	_, err := os.Stat(fn)
	if err != nil && os.IsNotExist(err) {
		fmt.Println("INFO: Preemptively pulling workload images. This only happens on first execution and aims to avoid delays opening apps.")

		_ = pullWorkloadImages(bin, cfg)
		_ = os.WriteFile(fn, []byte{}, files.FileMode)
	}
}

// PullAll refreshes every image in a config.
//
// Profile images are pulled whether or not the store already holds them,
// because refreshing is the whole point of the command behind this.
func PullAll(bin string, cfg *types.Config) error {
	s := NewStore()
	for _, img := range ProfileImages(cfg) {
		if _, err := refreshProfileImage(s, img); err != nil {
			slog.Error("cannot pull profile image", "image", img, "error", err)
		}
	}

	return pullWorkloadImages(bin, cfg)
}

func pullWorkloadImages(bin string, cfg *types.Config) error {
	imgs, err := UniqueImages(cfg)
	if err != nil {
		return fmt.Errorf("cannot get images: %w", err)
	}

	for _, img := range imgs {
		if err := PullImage(bin, img); err != nil {
			slog.Error("cannot pull workload image", "image", img, "error", err)
		}
	}

	return nil
}

// PullProfileImage returns the bundle for a profile image, pulling it into
// the OCI store only when the store cannot already provide it.
//
// Workload images still go through the container runner, because workloads
// still run under it. The two stores coexist until workloads move.
func PullProfileImage(ref string) (Bundle, error) {
	return pullProfileImage(NewStore(), ref)
}

// pullProfileImage skips the pull for an image the store already holds.
//
// A warm store lets a profile start with no network, which is what the
// runner backed path gave through PullImageIfNotPresent. Refreshing is
// PullAll's job, not a side effect of starting a profile.
func pullProfileImage(s *Store, ref string) (Bundle, error) {
	if b, err := s.Resolve(ref); err == nil {
		slog.Debug("profile image is already in the store", "image", ref)
		return b, nil
	}

	return refreshProfileImage(s, ref)
}

// refreshProfileImage pulls and unpacks ref whether or not the store
// already holds it.
func refreshProfileImage(s *Store, ref string) (Bundle, error) {
	if err := s.Pull(ref); err != nil {
		return Bundle{}, err
	}

	return s.Unpack(ref)
}

// ProfileImages returns the unique profile images in a config.
//
// Profile images live in the OCI store and workload images live in the
// container runner's store, because only profiles have moved to bwrap. The
// two coexist until workloads follow.
func ProfileImages(cfg *types.Config) []string {
	if cfg == nil {
		return nil
	}

	seen := map[string]struct{}{}
	imgs := make([]string, 0, len(cfg.Profiles))

	for _, p := range cfg.Profiles {
		if p.Image == "" {
			continue
		}
		if _, ok := seen[p.Image]; ok {
			continue
		}
		seen[p.Image] = struct{}{}
		imgs = append(imgs, p.Image)
	}

	return imgs
}

func PullImage(bin, image string) error {
	slog.Info("pulling container image", "image", image)
	cmd := execabs.Command(bin, "pull", image)
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

func PullImageIfNotPresent(bin, image string) error {
	ok, err := imagePresent(bin, image)
	if ok && err == nil {
		return nil
	}

	return PullImage(bin, image)
}

func imagePresent(bin, image string) (found bool, err error) {
	defer func() {
		slog.Debug("checking container image presence", "image", image, "found", found)
	}()
	cmd := execabs.Command(bin, "images", "-q", image)

	out, err := cmd.Output()
	if len(out) > 0 && err == nil {
		found = true
		return
	}

	return
}

func MissingImages(bin string, cfg *types.Config) ([]string, error) {
	imgs, err := UniqueImages(cfg)
	if err != nil {
		return nil, fmt.Errorf("cannot get images: %w", err)
	}

	missing := make([]string, 0, len(imgs))
	for _, img := range imgs {
		ok, err := imagePresent(bin, img)
		if ok && err == nil {
			continue
		}

		missing = append(missing, img)
	}

	return missing, nil
}

func UniqueImages(cfg *types.Config) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	missing := []string{}

	seen := map[string]struct{}{}

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

		if _, ok := seen[w.Image]; !ok {
			seen[w.Image] = struct{}{}
			missing = append(missing, w.Image)
		}
	}

	return missing, nil
}
