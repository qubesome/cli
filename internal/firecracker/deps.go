package firecracker

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/execabs"
)

const (
	command             = "firecracker"
	runUserDir          = "/run/user/%d"
	qubesomeDir         = "qubesome"
	qubesomeFilemode    = 0o700
	qubesomeCfgFilemode = 0o600

	kernelUrl  = "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.5/x86_64/vmlinux-5.10.186"
	kernelFile = "vmlinux"

	MB              = 1024 * 1024
	maxDownloadSize = 100 * MB

	networkDevName = "tap1"
)

func ensureDependencies(img string) error {
	if _, err := exec.LookPath(command); err != nil {
		return err
	}

	uid := os.Getuid()
	baseDir := fmt.Sprintf(runUserDir, uid)

	_, err := os.Stat(baseDir)
	if err != nil {
		return err
	}

	d := filepath.Join(baseDir, qubesomeDir)
	if err = os.MkdirAll(d, qubesomeFilemode); err != nil {
		return nil
	}

	kfile := filepath.Join(d, kernelFile)
	_, err = os.Stat(kfile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		slog.Info("cached kernel image not found")
		err = download(kernelUrl, kfile)
		if err != nil {
			return fmt.Errorf("failed to download kernel image: %w", err)
		}
	}

	_, err = net.InterfaceByName(networkDevName)
	if err != nil {
		return setupTaps(img)
	}

	return nil
}

func setupTaps(img string) error {
	slog.Info("setting up taps")
	cmd := execabs.Command("docker",
		"run", "--rm", "--privileged",
		"--network", "host",
		img,
		"setup_taps",
	)

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func download(url, target string) error {
	slog.Info("downloading file", "url", url, "target", target)

	f, err := os.Create(target)
	if err != nil {
		return err
	}
	defer f.Close()

	r, err := http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", r.Status)
	}

	if _, err = io.Copy(f, io.LimitReader(r.Body, maxDownloadSize)); err != nil {
		return err
	}

	return nil
}
