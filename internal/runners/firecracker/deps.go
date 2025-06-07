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
	"strconv"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/util/dbus"
	"golang.org/x/sys/execabs"

	_ "embed"
)

//go:embed firecracker.config
var configTmpl string

const (
	// kernelUrl from https://s3.amazonaws.com/spec.ccfc.min/
	kernelURL  = "https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.11/x86_64/vmlinux-6.1.102"
	kernelFile = "vmlinux"

	// light-weight image that contains the necessary tools for setting up
	// firecracker's network taps.
	firecrackerImg = "ghcr.io/qubesome/firecracker:latest"

	MB              = 1024 * 1024
	maxDownloadSize = 100 * MB

	networkDevName = "tap1"
)

func ensureDependencies() error {
	if _, err := exec.LookPath(files.FireCrackerBinary); err != nil {
		return err
	}

	d := files.QubesomeDir()
	if err := os.MkdirAll(d, files.DirMode); err != nil {
		return err
	}

	kfile := filepath.Join(d, kernelFile)
	_, err := os.Stat(kfile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		dbus.NotifyOrLog("firecracker", "downloading fresh kernel image")
		err = download(kernelURL, kfile)
		if err != nil {
			return fmt.Errorf("failed to download kernel image: %w", err)
		}
	}

	_, err = net.InterfaceByName(networkDevName)
	if err != nil {
		return setupTaps()
	}

	return nil
}

func createRootFs(dir, img string) (string, error) {
	slog.Info("creating root fs")
	rootfs := filepath.Join(dir, "roofs.ext4")
	bin := files.ContainerRunnerBinary("docker")
	cmd := execabs.Command(bin,
		"run", "--rm", "--privileged",
		"-v", dir+":"+dir,
		img,
		"create_rootfs", rootfs, strconv.Itoa(os.Getuid()),
	)

	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	if err := cmd.Run(); err != nil {
		return "", err
	}

	return rootfs, nil
}

func setupTaps() error {
	slog.Info("setting up taps")
	bin := files.ContainerRunnerBinary("docker")

	slog.Debug("setting up taps", "device name", networkDevName)
	cmd := execabs.Command(bin,
		"run", "--rm", "--privileged",
		"--network", "host",
		"-e", fmt.Sprintf("TAP_DEV=%s", networkDevName),
		firecrackerImg,
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

	r, err := http.Get(url) //nolint
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
