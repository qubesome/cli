package types

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/qubesome/cli/internal/files"
	"golang.org/x/sys/execabs"
	"gopkg.in/yaml.v3"
)

type WorkloadPullMode string

const (
	// OnDemand is a no-op and won't preemptively pull workload images.
	// This is the default behaviour.
	OnDemand WorkloadPullMode = "on-demand"
	// Background downloads all workload images on the background when
	// any command is executed. This operation will only take place once
	// a day.
	Background WorkloadPullMode = "background"
)

func (o WorkloadPullMode) Pull(wg *sync.WaitGroup) error {
	switch o {
	case Background:
		wg.Add(1)
		go func() {
			if exp, _ := pullExpired(); exp {
				err := PullAll()
				if err != nil {
					slog.Error("error pulling images", "error", err)
				}
			}
			wg.Done()
		}()
	case OnDemand:
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

func PullAll() error {
	matches, err := files.WorkloadFiles()
	if err != nil {
		return fmt.Errorf("cannot get workloads files: %w", err)
	}

	if len(matches) == 0 {
		fmt.Println("no workloads found")
	}

	seen := map[string]struct{}{}
	for _, fn := range matches {
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

		w := Workload{}
		err = yaml.Unmarshal(data, &w)
		if err != nil {
			return fmt.Errorf("cannot unmarshal workload file %q: %w", fn, err)
		}

		if _, ok := seen[w.Image]; !ok {
			seen[w.Image] = struct{}{}

			err = pull(w.Image)
			if err != nil {
				slog.Error("cannot pull image %q: %w", w.Image, err)
			}
		}
	}

	return nil
}

func pull(image string) error {
	slog.Debug("pulling workload image", "image", image)
	cmd := execabs.Command(files.DockerBinary, "pull", image)

	return cmd.Run()
}
