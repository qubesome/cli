package firecracker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"text/template"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
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
	bin := files.ContainerRunnerBinary("")
	cmd := execabs.Command(bin, 
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
	slog.Warn("use of firecracker is experimental")

	if err := ew.Validate(); err != nil {
		return err
	}

	if ew.Workload.SingleInstance {
		return fmt.Errorf("firecracker does not support single instance")
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

	params := configParams{
		KernelImagePath: kfile,
		RootFsPath:      rootfs,
		HostDeviceName:  networkDevName,
	}

	cfgPath := filepath.Join(d, "firecracker.cfg")

	slog.Debug("writing firecracker config file", "path", cfgPath)
	t, err := template.New("config").Parse(configTmpl)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(cfgPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, qubesomeCfgFilemode)
	if err != nil {
		return fmt.Errorf("failed to open firecracker config file: %w", err)
	}

	if err := t.Execute(f, params); err != nil {
		return fmt.Errorf("failed to create firecracker config contents: %w", err)
	}

	args := []string{
		"--api-sock",
		filepath.Join(d, "firecracker.sock"),
		"--config-file",
		cfgPath,
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
