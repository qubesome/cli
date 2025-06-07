package firecracker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

func Run(ew types.EffectiveWorkload) error {
	slog.Warn("use of firecracker is experimental")

	if err := ew.Validate(); err != nil {
		return err
	}

	if ew.Workload.SingleInstance {
		return fmt.Errorf("firecracker does not support single instance")
	}

	if err := ensureDependencies(); err != nil {
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

	kfile := filepath.Join(files.QubesomeDir(), kernelFile)
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

	f, err := os.OpenFile(cfgPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, files.FileMode)
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
	slog.Debug(files.FireCrackerBinary, "args", args)

	ctx := context.Background()
	cmd := execabs.CommandContext(ctx, files.FireCrackerBinary, args...)

	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	return cmd.Run()
}
