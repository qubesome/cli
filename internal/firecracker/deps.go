package firecracker

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

	networkIName = "tap0"
	networkCIDR  = "172.16.0.1/30"
)

func ensureDependencies() error {
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
		if !os.IsNotExist(err) {
			return err
		}

		slog.Info("cached kernel image not found")
		err = download(kernelUrl, kfile)
		if err != nil {
			return fmt.Errorf("failed to download kernel image: %w", err)
		}
	}

	_, err = net.InterfaceByName(networkIName)
	if err != nil {
		fmt.Printf(`Tap device for firecracker does not exist.
Create it using the commands below then try again:

	sudo ip tuntap add dev %[1]s mode tap
	sudo ip addr add %[2]s dev %[1]s
	sudo ip link set dev %[1]s up

`,
			networkIName, networkCIDR)
		return fmt.Errorf("tap interface %q not found", networkIName)
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
