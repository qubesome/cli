package firecracker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/env"
	"golang.org/x/sys/execabs"
)

// dataDisk is a machine's persistent disk, on the host.
//
// It is the only thing in this front that outlives a boot. The rootfs is
// rebuilt from the image bundle every time and discarded on shutdown, so
// everything a guest is meant to keep is on this one file.
type dataDisk struct {
	// Path is the image file, with any variables already expanded.
	Path string

	// SizeMiB is the size it is created at, and only ever created at.
	// See warnDataSize.
	SizeMiB int

	// cmdRunner is a test seam for the format. Production shells out to
	// mkfs.ext4 through runMkfs, and tests substitute a fake so the
	// create, reuse and failure paths can be verified without e2fsprogs
	// installed. It is the seam images.Store uses, in the same shape,
	// because it is the same problem.
	cmdRunner func(bin string, args []string) error
}

// run invokes cmdRunner if a test has set one, and runMkfs otherwise.
func (d dataDisk) run(bin string, args []string) error {
	if d.cmdRunner != nil {
		return d.cmdRunner(bin, args)
	}
	return runMkfs(bin, args)
}

// EnsureDataDisk prepares a machine's persistent disk and returns its
// path on the host.
//
// A disk that is not there is created sparse and formatted empty. One
// that is there is used exactly as it is found. See warnDataSize.
func EnsureDataDisk(data types.MicroVMData) (string, error) {
	d := dataDisk{Path: env.Expand(data.Path), SizeMiB: data.SizeMiB}

	if err := d.ensure(); err != nil {
		return "", err
	}

	return d.Path, nil
}

func (d dataDisk) ensure() error {
	fi, err := os.Stat(d.Path)
	if err == nil {
		if fi.IsDir() {
			return fmt.Errorf("the microVM data disk %q is a directory", d.Path)
		}

		warnDataSize(d.Path, fi.Size(), d.bytes())

		return nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read the microVM data disk %q: %w", d.Path, err)
	}

	// The directory the disk is configured into is made rather than
	// required, because it holds nothing but disks qubesome creates and a
	// launch that failed over a missing parent would only ever be
	// answered by making it by hand.
	if err := os.MkdirAll(filepath.Dir(d.Path), files.DirMode); err != nil {
		return fmt.Errorf("failed to create the directory of the microVM data disk %q: %w", d.Path, err)
	}

	if err := createSparse(d.Path, d.bytes()); err != nil {
		return err
	}

	if err := d.run(files.MkfsExt4Binary, mkfsDataArgs(d.Path)); err != nil {
		// A file that was created and not formatted must not be left
		// behind. The next launch would find it, take it for a disk that
		// already exists, and hand the guest a mount that fails or, worse,
		// whatever was in those blocks.
		if rerr := os.Remove(d.Path); rerr != nil {
			slog.Warn("failed to remove a microVM data disk that could not be formatted",
				"path", d.Path, "error", rerr)
		}

		return fmt.Errorf("failed to format the microVM data disk %q: %w", d.Path, err)
	}

	return nil
}

func (d dataDisk) bytes() int64 {
	return int64(d.SizeMiB) * mib
}

// warnDataSize reports a configured size that no longer matches the disk.
//
// An existing disk is never reformatted and never resized. A shrink
// throws away everything past the new end, and a grow needs the
// filesystem resized inside the file as well as the file itself, so
// neither happens quietly on the strength of a number in a config that
// may have been changed for a different machine entirely.
//
// It is a warning rather than an error because the disk still works and
// refusing would take the workload down over a field that describes how
// it was created. Both numbers are named, because the alternative is a
// sizeMiB that has silently meant nothing since the first boot.
func warnDataSize(path string, got, want int64) {
	if got == want {
		return
	}

	slog.Warn("the microVM data disk is not the configured size, and is left as it is",
		"path", path, "configuredMiB", want/mib, "actualMiB", got/mib)
}

// mkfsDataArgs formats an empty persistent disk.
//
// It runs on the host and not in the guest because the guest is an
// arbitrary OCI image, and e2fsprogs is not something an image chosen
// freely can be assumed to ship. The host already needs mkfs.ext4 to
// build a rootfs at all.
//
// The journal is kept, which is the difference from the rootfs. That
// image is rebuilt on every boot and thrown away on shutdown, so a
// journal there is a write nothing would ever replay. This one is the
// only durable thing in a machine, and the journal is what keeps it
// mountable after a host that lost power. Reserved blocks are still zero,
// since nothing in a guest runs as a user that a reserve would help. The
// size comes from the file, which was created at it just above.
func mkfsDataArgs(path string) []string {
	return []string{
		"-q", "-F",
		"-m", "0",
		"-E", "lazy_itable_init=1,lazy_journal_init=1",
		path,
	}
}

// runMkfs shells out to the host's mkfs.ext4.
func runMkfs(bin string, args []string) error {
	slog.Debug("exec", "binary", bin, "args", args)

	//nolint:gosec // G204: fixed binary, arguments built by mkfsDataArgs.
	cmd := execabs.CommandContext(context.Background(), bin, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if msg := bytes.TrimSpace(stderr.Bytes()); len(msg) > 0 {
			return fmt.Errorf("%s %v: %w: %s", filepath.Base(bin), args, err, msg)
		}
		return fmt.Errorf("%s %v: %w", filepath.Base(bin), args, err)
	}

	return nil
}
