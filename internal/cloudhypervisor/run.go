package cloudhypervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/qubesome/qubesome-cli/internal/types"
	"golang.org/x/sys/execabs"
)

type configParams struct {
	KernelImagePath string
	RootFsPath      string
	HostDeviceName  string
}

func createRootFs(dir, img string) (string, error) {
	slog.Info("creating root fs")
	rootfs := filepath.Join(dir, "roofs.ext4")
	cmd := execabs.Command("docker",
		"run", "--rm", "--privileged",
		"-v", "/tmp/:/tmp/",
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

func Run(ew types.EffectiveWorkload) error {
	if err := ew.Validate(); err != nil {
		return err
	}

	if ew.Workload.SingleInstance {
		return fmt.Errorf("cloud-hypervisor does not support single instance")
	}

	if err := ensureDependencies(ew.Workload.Image); err != nil {
		return err
	}

	d, err := os.MkdirTemp("", "qubesome-")
	if err != nil {
		return err
	}

	rootfs, err := createRootFs(d, ew.Workload.Image)
	if err != nil {
		return err
	}

	uid := os.Getuid()
	baseDir := fmt.Sprintf(runUserDir, uid)
	kfile := filepath.Join(baseDir, qubesomeDir, kernelFile)

	args := []string{
		"--kernel", kfile,
		"--disk", fmt.Sprintf("path=%s", rootfs),
		"--cmdline", "console=hvc0 root=/dev/vda1 rw",
		"--cpus", "boot=4",
		"--memory", "size=1024M",
		"--net", "tap=,mac=,ip=,mask=",
	}

	return run(args)
}

func run(args []string) error {
	slog.Debug(command, "args", args)

	ctx := context.Background()
	cmd := execabs.CommandContext(ctx, command, args...)

	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	return cmd.Run()
}
