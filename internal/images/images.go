package images

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"golang.org/x/sys/execabs"
	"gopkg.in/yaml.v3"
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
		wg.Add(1)
		go func() {
			if exp, _ := pullExpired(); exp {
				err := PullAll(bin, cfg)
				if err != nil {
					slog.Error("error pulling images", "error", err)
				}
			}
			wg.Done()
		}()
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

		_ = PullAll(bin, cfg)
		_ = os.WriteFile(fn, []byte{}, files.FileMode)
	}
}

func PullAll(bin string, cfg *types.Config) error {
	imgs, err := UniqueImages(cfg)
	if err != nil {
		return fmt.Errorf("cannot get images: %w", err)
	}

	for _, img := range imgs {
		err = PullImage(bin, img)
		if err != nil {
			slog.Error("cannot pull image %q: %w", img, err)
		}
	}

	return nil
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
	for _, p := range cfg.Profiles {
		if _, ok := seen[p.Image]; !ok {
			seen[p.Image] = struct{}{}
			missing = append(missing, p.Image)
		}
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
		err = yaml.Unmarshal(data, &w)
		if err != nil {
			return nil, fmt.Errorf("cannot unmarshal workload file %q: %w", fn, err)
		}

		if _, ok := seen[w.Image]; !ok {
			seen[w.Image] = struct{}{}
			missing = append(missing, w.Image)
		}
	}

	return missing, nil
}
