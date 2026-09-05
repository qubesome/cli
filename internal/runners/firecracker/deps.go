package firecracker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"time"

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
	// kernelSHA256 pins the kernel the VMs boot. Upstream publishes no
	// digest alongside the artifact, so this one was taken from a download
	// of it. Update it together with kernelURL.
	kernelSHA256 = "cf42303c29e8c4a02798f357ba056c5567baf074aaed4eec78c997fb9df08cf9"

	// light-weight image that contains the necessary tools for setting up
	// firecracker's network taps.
	firecrackerImg = "ghcr.io/qubesome/firecracker:latest"

	MB              = 1024 * 1024
	maxDownloadSize = 100 * MB
	downloadTimeout = 10 * time.Minute

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
		err = download(kernelURL, kfile, kernelSHA256)
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

func download(url, target, wantSHA256 string) error {
	slog.Info("downloading file", "url", url, "target", target)

	// Decode the expected digest up front. Comparing the bytes rather than
	// the strings takes the case of the hex out of the question, and a
	// value that is not a SHA-256 digest is a mistake worth reporting
	// before anything is downloaded.
	want, err := hex.DecodeString(wantSHA256)
	if err != nil || len(want) != sha256.Size {
		return fmt.Errorf("invalid expected checksum %q for %s", wantSHA256, url)
	}

	// The kernel is only downloaded when it is missing, so whatever lands
	// at target is trusted from then on. Write to a temporary file next to
	// it and only move it into place once it has been verified, so an
	// interrupted or truncated download is never mistaken for a complete
	// one. The name is derived from the target rather than random, so a
	// download killed before its cleanup runs leaves one file that the
	// next attempt truncates, instead of a new one every time.
	part := target + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, files.FileMode)
	if err != nil {
		return err
	}

	// The file is closed explicitly once it has been written, so that a
	// failure to flush is reported rather than discarded. This only cleans
	// up after the paths that return early.
	defer func() {
		if f != nil {
			_ = f.Close()
			_ = os.Remove(part)
		}
	}()

	// Requests carry no deadline of their own, so a connection that stalls
	// would hang dependency setup for as long as the peer keeps it open.
	// The deadline covers reading the body, not just the response.
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", r.Status)
	}

	h := sha256.New()

	// Read one byte past the limit so that a body of exactly the limit is
	// told apart from one that was cut short by it.
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r.Body, maxDownloadSize+1))
	if err != nil {
		return err
	}
	if n > maxDownloadSize {
		return fmt.Errorf("download is larger than the %d byte limit", int64(maxDownloadSize))
	}

	got := h.Sum(nil)
	if !bytes.Equal(got, want) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s",
			url, hex.EncodeToString(got), wantSHA256)
	}

	if err := f.Chmod(files.FileMode); err != nil {
		return err
	}

	// Closing a file that was written to can fail, and the failure means
	// the contents are not what was verified above.
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", part, err)
	}
	f = nil

	return os.Rename(part, target)
}
