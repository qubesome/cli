package deps

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"text/tabwriter"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
)

var (
	red   = "\033[31m"
	green = "\033[32m"
	amber = "\033[33m"
	reset = "\033[0m"
)

// imageTools fill the OCI store. skopeo fetches an image and umoci
// unpacks it into the root filesystem a sandbox is built on.
var imageTools = []string{
	files.SkopeoBinary,
	files.UmociBinary,
}

// sandboxTools are what opening a profile or a workload needs: the image
// tools, and bwrap to build the sandbox around what they unpacked.
//
// They are derived from one list rather than written out twice because
// the table drifted apart once already, with run, xdg-open and images
// asking for a container runner long after nothing used one.
var sandboxTools = append([]string{files.BwrapBinary}, imageTools...)

var deps map[string][]string = map[string][]string{
	"clip": {
		files.XclipBinary,
		files.ShBinary,
	},
	"run":      sandboxTools,
	"xdg-open": sandboxTools,
	// Filling the store fetches and unpacks. Nothing is launched, so
	// this is the one sandbox command that does not need bwrap.
	"images": imageTools,
	// A profile needs the same three and two of its own. sh is what the
	// profile sandbox runs as its init, and xrandr reads the host screen
	// geometry the profile is sized against.
	"start": append(slices.Clone(sandboxTools), files.ShBinary, files.XrandrBinary),
}

// optionalDeps are the binaries only an optional feature needs. They are
// reported in amber, so a host that does not use the feature does not
// read the table as broken.
//
// firecracker is the one runner left besides bwrap. It boots a machine
// on a root filesystem built by mkfs.ext4 out of the same bundle a
// sandbox is built on, so it needs its own binary and e2fsprogs. bwrap
// is not listed beside them because it is already required above: the
// rootfs build is a bwrap invocation too, and a host without bwrap
// cannot run a workload at all.
//
// Only run and xdg-open list them. A profile is always a bwrap sandbox
// and never a machine. images is the interesting omission, since a
// machine's rootfs is now built from a bundle that store unpacked, so
// the two are no longer unrelated. It stays out all the same: filling
// the store is a fetch and an unpack, which is what imageTools does, and
// none of it is a rootfs build. A host that only ever runs qubesome
// images would be told to install a VMM it has no use for.
var optionalDeps map[string][]string = map[string][]string{
	"run": {
		files.FireCrackerBinary,
		files.MkfsExt4Binary,
	},
	"xdg-open": {
		files.FireCrackerBinary,
		files.MkfsExt4Binary,
	},
	// The profile compositor decides the keymap for everything inside it,
	// and without this its layout is whatever libxkbcommon defaults to
	// rather than the one being typed on. Optional because a profile
	// still starts, on the wrong layout.
	"start": {
		files.SetxkbmapBinary,
	},
}

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 5, ' ', 0)
	fmt.Fprintln(writer, "Command\tDependency\tStatus")
	fmt.Fprintln(writer, "-------\t----------\t------")

	for name, d := range deps {
		for _, dn := range d {
			_, err := exec.LookPath(dn)
			status := green + "OK" + reset
			if err != nil {
				status = red + "NOT FOUND" + reset
			}

			fmt.Fprintf(writer, "%s\t%s\t%s\n", name, dn, status)
		}

		if opt, ok := optionalDeps[name]; ok {
			for _, dn := range opt {
				_, err := exec.LookPath(dn)
				status := green + "OK" + reset
				if err != nil {
					status = amber + "NOT FOUND (Optional)" + reset
				}

				fmt.Fprintf(writer, "%s\t%s\t%s\n", name, dn, status)
			}
		}
	}

	sandboxChecks(writer)
	writer.Flush()
	fmt.Println()

	if o.Config == nil {
		fmt.Println("images not checked: qubesome config not found")
		return nil
	}

	imgs, err := images.MissingImages(o.Config)
	if err != nil {
		return err
	}

	writer = tabwriter.NewWriter(os.Stdout, 0, 0, 5, ' ', 0)
	fmt.Fprintln(writer, "Image\tStatus")
	fmt.Fprintln(writer, "-------\t------")
	for _, img := range imgs {
		status := amber + "Missing" + reset

		fmt.Fprintf(writer, "%s\t%s\n", img, status)
	}

	writer.Flush()

	return nil
}

// readSysctl reads an integer from a sysctl file under /proc.
func readSysctl(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("failed to read %q: %w", path, err)
	}

	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse %q: %w", path, err)
	}

	return n, nil
}

// hasUserACL reports whether getfacl output grants a named user access.
//
// systemd-logind grants the seat's user an ACL on the render node, the
// camera and the sound devices through uaccess, and ACLs are evaluated
// against the fsuid, which bwrap maps. That is what makes a single uid
// mapping enough, so its absence is worth reporting.
func hasUserACL(acl, username string) bool {
	for _, line := range strings.Split(acl, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "user:"+username+":") {
			return true
		}
	}

	return false
}

// deviceGlobs lists the device nodes a profile sandbox relies on a
// uaccess ACL for. bwrap's unprivileged mode maps a single uid and gid,
// so --group-add=video and --group-add=audio resolve to nothing inside
// the sandbox. Only the ACL systemd-logind grants the seat's user gets
// the render node, the camera and the sound card through. One glob per
// class of device reports one row per card rather than one per subdevice
// node, which would otherwise drown the table in /dev/snd/* entries.
var deviceGlobs = []string{
	"/dev/dri/renderD*",
	"/dev/video*",
	"/dev/snd/controlC*",
}

// sandboxCommands names the commands the checks below apply to.
//
// It used to read "start", when a profile was the only sandbox. A
// workload is one too now, so run and xdg-open depend on the same host
// settings. images is not here: it fills the OCI store and opens nothing.
const sandboxCommands = "run, start, xdg-open"

// sandboxChecks reports the host settings a bwrap sandbox depends on.
//
// These are not binaries, so they do not belong in the dependency table,
// but each of them turns into a confusing failure much later if it is
// wrong. Only max_user_namespaces is fatal. The rest are warnings.
func sandboxChecks(writer io.Writer) {
	fmt.Fprintln(writer, "\nSandbox\tRequirement\tStatus")
	fmt.Fprintln(writer, "-------\t-----------\t------")

	n, err := readSysctl("/proc/sys/user/max_user_namespaces")
	switch {
	case err != nil:
		fmt.Fprintf(writer, "%s\tuser namespaces\t%sUNKNOWN: %v%s\n", sandboxCommands, amber, err, reset)
	case n == 0:
		fmt.Fprintf(writer, "%s\tuser namespaces\t%sDISABLED: use the setuid bwrap%s\n", sandboxCommands, red, reset)
	default:
		fmt.Fprintf(writer, "%s\tuser namespaces\t%sOK%s\n", sandboxCommands, green, reset)
	}

	// Debian and Ubuntu restrict the nested user namespaces Chromium's own
	// sandbox needs. Absent elsewhere, so a missing file is not a finding.
	const apparmorPath = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"
	if restricted, err := readSysctl(apparmorPath); err == nil && restricted != 0 {
		fmt.Fprintf(writer, "%s\tapparmor userns\t%sRESTRICTED: use the setuid bwrap%s\n", sandboxCommands, amber, reset)
	}

	deviceACLChecks(writer)
}

// deviceACLChecks reports whether the current user has the uaccess ACL
// that systemd-logind grants on each device a profile can use.
//
// In unprivileged mode bwrap maps a single uid and gid, so supplementary
// groups resolve to nogroup inside the sandbox: the ACL, evaluated
// against the fsuid that bwrap does map, is what stands in for group
// membership. Where it is missing the profile silently drops to software
// rendering, which is precisely the failure this project exists to
// remove, so it is worth reporting as a warning.
func deviceACLChecks(writer io.Writer) {
	if _, err := exec.LookPath(files.GetfaclBinary); err != nil {
		fmt.Fprintf(writer, "%s\tdevice ACLs\t%sUNKNOWN: %s not found%s\n", sandboxCommands, amber, files.GetfaclBinary, reset)
		return
	}

	// user.Current reads the real uid the process runs as. USER can be
	// unset or spoofed, and it is this uid's fsuid the ACL is evaluated
	// against, not whatever a launcher happened to put in the environment.
	u, err := user.Current()
	if err != nil {
		fmt.Fprintf(writer, "%s\tdevice ACLs\t%sUNKNOWN: %v%s\n", sandboxCommands, amber, err, reset)
		return
	}

	var devices []string
	for _, pattern := range deviceGlobs {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		devices = append(devices, matches...)
	}

	ctx := context.Background()
	for _, dev := range devices {
		out, err := exec.CommandContext(ctx, files.GetfaclBinary, "-p", dev).Output() //nolint:gosec // dev comes from a fixed device glob, not user input.
		if err != nil {
			fmt.Fprintf(writer, "%s\t%s acl\t%sUNKNOWN: %v%s\n", sandboxCommands, dev, amber, err, reset)
			continue
		}

		if hasUserACL(string(out), u.Username) {
			fmt.Fprintf(writer, "%s\t%s acl\t%sOK%s\n", sandboxCommands, dev, green, reset)
			continue
		}

		fmt.Fprintf(writer, "%s\t%s acl\t%sNO uaccess ACL: expect software rendering%s\n",
			sandboxCommands, dev, amber, reset)
	}
}
