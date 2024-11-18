package images

import (
	"errors"
	"fmt"
	"io/fs"
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

	slog.Debug("images.Run", "options", o)
	return PullAll(o.Config)
}

func Pull(cfg *types.Config, wg *sync.WaitGroup) error {
	switch cfg.WorkloadPullMode {
	case types.Background:
		wg.Add(1)
		go func() {
			if exp, _ := pullExpired(); exp {
				err := PullAll(cfg)
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
		if !errors.Is(err, fs.ErrNotExist) {
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

func PullAll(cfg *types.Config) error {
	wf, err := cfg.WorkloadFiles()
	if err != nil {
		return fmt.Errorf("cannot get workloads files: %w", err)
	}

	if len(wf) == 0 {
		fmt.Println("no workloads found")
	}

	seen := map[string]struct{}{}
	for _, fn := range wf {
		fi, err := os.Stat(fn)
		if err != nil {
			return fmt.Errorf("cannot stat file %q: %w", fn, err)
		}

		if !fi.Mode().IsRegular() {
			continue
		}

		data, err := os.ReadFile(fn)
		if err != nil {
			return fmt.Errorf("cannot read file %q: %w", fn, err)
		}

		w := types.Workload{}
		err = yaml.Unmarshal(data, &w)
		if err != nil {
			return fmt.Errorf("cannot unmarshal workload file %q: %w", fn, err)
		}

		if _, ok := seen[w.Image]; !ok {
			seen[w.Image] = struct{}{}

			err = PullImage(w.Image)
			if err != nil {
				slog.Error("cannot pull image %q: %w", w.Image, err)
			}
		}
	}

	return nil
}

func PullImage(image string) error {
	slog.Info("pulling container image", "image", image)
	cmd := execabs.Command(files.ContainerRunnerBinary, "pull", image) //nolint
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

func PullImageIfNotPresent(image string) error {
	slog.Debug("checking if container image is present", "image", image)
	cmd := execabs.Command(files.ContainerRunnerBinary, "images", "-q", image) //nolint

	out, err := cmd.Output()
	if len(out) > 0 && err == nil {
		return nil
	}

	return PullImage(image)
}
