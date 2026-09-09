package firecracker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/env"
	"golang.org/x/sys/execabs"
)

const (
	// composedRoot is where the tree that becomes the guest root
	// filesystem is assembled. It is a mount point inside a throwaway
	// namespace, so nothing of the host is hidden by it.
	composedRoot = "/mnt"

	// guestInit is where the qubesome binary lands in the guest. The
	// kernel command line names the same path with init=, so the two move
	// together.
	guestInit = "/sbin/qubesome-init"

	// guestInitConfig is where the guest init reads what to run.
	guestInitConfig = "/etc/qubesome/init.json"

	// initConfigFile is the name the same file has on the host, next to
	// the image it is written into.
	initConfigFile = "init.json"

	// separator ends bwrap's own options and begins the command it runs.
	separator = "--"

	mib = 1024 * 1024
)

// InitConfig is what the guest init is given, as
// /etc/qubesome/init.json in the composed tree.
//
// It is a file rather than kernel command line arguments. The command line
// is bounded in length, and everything on it is world readable inside the
// guest through /proc/cmdline, while the tree is being composed anyway so a
// file costs nothing.
type InitConfig struct {
	// Argv is the command the guest runs, already resolved from the
	// workload. An empty Argv is a machine that only ever runs what a
	// console asks it for.
	Argv []string `json:"argv"`

	// Env is the environment of that command, the image's own merged with
	// what qubesome adds.
	Env []string `json:"env"`

	// Cwd is its working directory, as the image declared it.
	Cwd string `json:"cwd"`

	// Hostname is the effective workload name, which is what the bwrap
	// runner gives its sandboxes too.
	Hostname string `json:"hostname"`

	// DataMount is where the guest mounts the persistent disk, and is
	// empty when the workload configured none. It is also how the guest
	// knows there is a second drive to mount at all.
	DataMount string `json:"dataMount,omitempty"`
}

// roPath is one host path composed into the guest tree. Both sides are
// resolved already: Src is expanded and Dst is the path in the guest,
// without the prefix the tree is assembled under.
type roPath struct {
	Src string
	Dst string
}

// rootfsBuild is one resolved root filesystem build.
type rootfsBuild struct {
	// Rootfs is the bundle's extracted tree, read as the lower layer of
	// the overlay and never written to.
	Rootfs string

	// Target is the ext4 image on the host.
	Target string

	// SizeMiB is the size the image is created at.
	SizeMiB int

	// Paths are the workload's read-only paths, composed over the image.
	Paths []roPath

	// QubesomeBin is the host binary that becomes the guest init.
	QubesomeBin string

	// InitConfig is the host path of the generated init.json.
	InitConfig string
}

// rootfsArgs renders a build into the arguments of the one command it
// runs, bwrap wrapping mkfs.ext4.
//
// Ordering carries meaning, and disturbing it writes a wrong tree rather
// than failing. --overlay-src names a layer that the next overlay option
// consumes, so it has to come immediately before the --tmp-overlay that
// mounts it. Every bind into the composed tree comes after that
// --tmp-overlay, because a mount hides whatever was put underneath it
// earlier and bubblewrap 0.11.2 discards those binds without a word.
// internal/sandbox/args.go documents the same class of hazard for the
// workload sandbox.
//
// The binds before the overlay are what mkfs.ext4 itself needs to run.
// This is a host tool being given a uid map, not a sandbox around
// untrusted code, so that list is the tool's dependencies and nothing was
// left out of it for isolation.
func rootfsArgs(b rootfsBuild) []string {
	args := make([]string, 0, 30+3*len(b.Paths))

	args = append(args,
		// The bundle is owned by the invoking user, because umoci unpacks
		// it rootless and cannot chown. Inside this map its files stat as
		// 0:0 with their mode bits intact, and that is what mkfs.ext4
		// writes into the inodes, so the guest gets a root owned image.
		// Measured on the target host, recorded in the header of
		// hack/verify-microvm-rootfs.sh: uid 0, mode 4755 kept on a
		// setuid binary, symlinks and hardlinks intact. Without it every
		// file in the guest belongs to uid 1000.
		"--unshare-user",
		"--uid", "0",
		"--gid", "0",

		// mkfs.ext4 lives in /usr/sbin and links against the host's
		// libraries. /bin, /sbin, /lib and /lib64 are symlinks into /usr
		// on a merged host and real directories elsewhere, so they are
		// bound if they are there.
		"--ro-bind", "/usr", "/usr",
		"--ro-bind-try", "/bin", "/bin",
		"--ro-bind-try", "/sbin", "/sbin",
		"--ro-bind-try", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",

		// mke2fs reads its feature defaults from this file. Without it
		// the built-in fallback decides which features the filesystem
		// gets, so the image would not be the one the host's e2fsprogs
		// was configured to build.
		"--ro-bind-try", "/etc/mke2fs.conf", "/etc/mke2fs.conf",

		// The image is the one thing the command writes, and it is
		// created before this runs so there is a file to bind.
		"--bind", b.Target, b.Target,

		// The bundle is shared. internal/images unpacks an image once per
		// manifest digest and every workload using that image runs
		// straight out of the same tree, the bwrap runner included, so a
		// write here would reach all of them. --overlay-src is what keeps
		// this build off it: the tree is read as a lower layer and every
		// change the composition makes lands in the overlay's tmpfs and
		// goes away with the namespace.
		"--overlay-src", b.Rootfs,
		"--tmp-overlay", composedRoot,
	)

	// Read-only paths are composed into the image before it is built
	// rather than shared into a running guest. bwrap creates the parent
	// directories the image does not have, so a path lands where the
	// workload asked for it whatever the image ships.
	for _, p := range b.Paths {
		args = append(args, "--ro-bind", p.Src, filepath.Join(composedRoot, p.Dst))
	}

	args = append(args,
		"--ro-bind", b.QubesomeBin, filepath.Join(composedRoot, guestInit),
		"--ro-bind", b.InitConfig, filepath.Join(composedRoot, guestInitConfig),

		separator,
		files.MkfsExt4Binary,

		// The image is rebuilt from the bundle on every boot and thrown
		// away on shutdown, so the journal is a write nothing will ever
		// replay and the reserved blocks are space the guest is denied
		// for a root that will never need it. The inode table and the
		// journal are left to be initialised lazily because nothing reads
		// either before the guest mounts the filesystem.
		"-q", "-F",
		"-m", "0",
		"-O", "^has_journal",
		"-E", "lazy_itable_init=1,lazy_journal_init=1",

		"-d", composedRoot,
		b.Target,
		strconv.Itoa(b.SizeMiB)+"M",
	)

	return args
}

// BuildRootfs writes the guest's root filesystem to target.
//
// What goes into it is the unpacked image with the workload's read-only
// paths, the qubesome binary and the guest init configuration composed
// over it. Nothing is copied and nothing is mounted on the host: the
// composition is a bwrap overlay and mkfs.ext4 -d walks it, inside a user
// namespace where the invoking uid is 0 so the inodes come out root owned.
//
// It is rebuilt on every boot and discarded on shutdown, which is what
// makes the read-only paths free. On the target host that cost 2.5 s over
// a 926M bundle, recorded in the header of hack/verify-microvm-rootfs.sh.
func BuildRootfs(bundle images.Bundle, ew types.EffectiveWorkload, target string) error {
	m := ew.Workload.MicroVM.WithDefaults()

	warnImageUser(ew.Workload.Image, bundle.UID)

	// The guest init is this binary, bound in rather than shipped by the
	// image. The release build is static, so it needs nothing from the
	// guest's loader.
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve the qubesome binary: %w", err)
	}

	cfg := filepath.Join(filepath.Dir(target), initConfigFile)
	if err := writeInitConfig(cfg, bundle, ew); err != nil {
		return err
	}

	if err := createSparse(target, int64(m.RootfsSizeMiB)*mib); err != nil {
		return err
	}

	args := rootfsArgs(rootfsBuild{
		Rootfs:      bundle.Rootfs,
		Target:      target,
		SizeMiB:     m.RootfsSizeMiB,
		Paths:       readOnlyPaths(ew.Workload.HostAccess.Paths),
		QubesomeBin: bin,
		InitConfig:  cfg,
	})

	slog.Debug(files.BwrapBinary, "args", args)

	//nolint:gosec // G204: fixed binary, arguments built by rootfsArgs.
	cmd := execabs.CommandContext(context.Background(), files.BwrapBinary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build the guest root filesystem at %q: %w", target, err)
	}

	return nil
}

// warnImageUser reports an image whose USER the machine will not honour.
//
// Only one uid is mapped into the namespace the tree is composed in, so
// root is the only ownership the build can express and the guest runs the
// workload as root whatever the image asked for. The build carries on,
// because the alternative is refusing most of the images on a registry.
// See the ownership section of
// docs/superpowers/specs/2026-09-08-firecracker-any-image-design.md for
// why a subuid range is not used and what it would take.
func warnImageUser(image string, uid int) {
	if uid == 0 {
		return
	}

	slog.Warn("image asks for a non-root user, the microVM runs it as root",
		"image", image, "uid", uid)
}

// createSparse creates target at size bytes with nothing in it.
//
// The file is sparse, so it costs what the guest writes rather than what
// it was sized at. The size is what the configuration asked for and is
// never derived from the bundle: a du of the tree is a guess at what the
// guest is going to need, and a guess that comes up short is an out of
// space at some later boot rather than an error here.
func createSparse(target string, size int64) error {
	f, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to create the guest root filesystem image: %w", err)
	}

	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to size the guest root filesystem image: %w", err)
	}

	return f.Close()
}

// writeInitConfig writes the guest init's configuration to path.
//
// It is written on the host and bound into the composed tree, so it is
// part of the image the guest boots rather than something handed over at
// runtime.
func writeInitConfig(path string, bundle images.Bundle, ew types.EffectiveWorkload) error {
	data, err := json.Marshal(initConfig(bundle, ew))
	if err != nil {
		return fmt.Errorf("failed to encode the guest init configuration: %w", err)
	}

	if err := os.WriteFile(path, data, files.FileMode); err != nil {
		return fmt.Errorf("failed to write the guest init configuration: %w", err)
	}

	return nil
}

// initConfig resolves what the guest runs.
//
// The bundle carries no entrypoint, exactly as it does not for the bwrap
// runner, so the argv is the workload's command. A firecracker workload is
// allowed to leave it empty, because a machine's reason to exist is the
// consoles that attach to it and those bring their own argv.
func initConfig(bundle images.Bundle, ew types.EffectiveWorkload) InitConfig {
	wl := ew.Workload

	var argv []string
	if wl.Command != "" {
		argv = append([]string{wl.Command}, wl.Args...)
	}

	vars := make([]string, 0, len(bundle.Env)+1)
	vars = append(vars, bundle.Env...)
	if ew.Profile != nil {
		vars = append(vars, "QUBESOME_PROFILE="+ew.Profile.Name)
	}

	var dataMount string
	if data := wl.MicroVM.WithDefaults().Data; data != nil {
		dataMount = data.Mount
	}

	return InitConfig{
		Argv:      argv,
		Env:       vars,
		Cwd:       bundle.Cwd,
		Hostname:  ew.Name,
		DataMount: dataMount,
	}
}

// readOnlyPaths resolves the workload's read-only path mappings.
//
// A mapping is src:dst with an optional :ro, and the halves are split
// rather than cut, because cutting at the first colon would leave the flag
// on the end of the destination. types.ValidateMicroVM already refused
// every mapping that is not read-only, since a write would land in a copy
// that the shutdown discards, so anything else here is dropped rather than
// composed in writable.
func readOnlyPaths(paths []string) []roPath {
	out := make([]roPath, 0, len(paths))

	for _, p := range paths {
		parts := strings.Split(p, ":")
		if len(parts) < 3 || parts[2] != "ro" {
			slog.Warn("path is not read-only and is left out of the microVM image", "path", p)
			continue
		}

		src := env.Expand(parts[0])
		if err := files.EnsureMappedDir(src); err != nil {
			slog.Warn("failed to compose path into the microVM image", "path", src, "error", err)
			continue
		}

		out = append(out, roPath{Src: src, Dst: parts[1]})
	}

	return out
}
