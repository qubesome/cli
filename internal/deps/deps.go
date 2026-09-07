package deps

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
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

var deps map[string][]string = map[string][]string{
	"clip": {
		files.XclipBinary,
		files.ShBinary,
	},
	"run": {
		files.PodmanBinary,
		files.DockerBinary,
	},
	"xdg-open": {
		files.PodmanBinary,
		files.DockerBinary,
	},
	"images": {
		files.PodmanBinary,
		files.DockerBinary,
	},
	"start": {
		files.PodmanBinary,
		files.DockerBinary,
		files.ShBinary,
		files.XrandrBinary,
		files.BwrapBinary,
		files.SkopeoBinary,
		files.UmociBinary,
	},
}

var optionalDeps map[string][]string = map[string][]string{
	"run": {
		files.FireCrackerBinary,
	},
	"xdg-open": {
		files.FireCrackerBinary,
	},
	"images": {
		files.FireCrackerBinary,
	},
	"start": {
		files.FireCrackerBinary,
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

	bin := files.ContainerRunnerBinary(o.Runner)
	imgs, err := images.MissingImages(bin, o.Config)
	if err != nil {
		return err
	}

	writer = tabwriter.NewWriter(os.Stdout, 0, 0, 5, ' ', 0)
	fmt.Fprintln(writer, "Image\tRunner\tStatus")
	fmt.Fprintln(writer, "-------\t----------\t------")
	for _, img := range imgs {
		status := amber + "Missing" + reset

		fmt.Fprintf(writer, "%s\t%s\t%s\n", img, bin, status)
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
		fmt.Fprintf(writer, "start\tuser namespaces\t%sUNKNOWN: %v%s\n", amber, err, reset)
	case n == 0:
		fmt.Fprintf(writer, "start\tuser namespaces\t%sDISABLED: use the setuid bwrap%s\n", red, reset)
	default:
		fmt.Fprintf(writer, "start\tuser namespaces\t%sOK%s\n", green, reset)
	}

	// Debian and Ubuntu restrict the nested user namespaces Chromium's own
	// sandbox needs. Absent elsewhere, so a missing file is not a finding.
	const apparmorPath = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"
	if restricted, err := readSysctl(apparmorPath); err == nil && restricted != 0 {
		fmt.Fprintf(writer, "start\tapparmor userns\t%sRESTRICTED: use the setuid bwrap%s\n", amber, reset)
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
		fmt.Fprintf(writer, "start\tdevice ACLs\t%sUNKNOWN: %s not found%s\n", amber, files.GetfaclBinary, reset)
		return
	}

	// user.Current reads the real uid the process runs as. USER can be
	// unset or spoofed, and it is this uid's fsuid the ACL is evaluated
	// against, not whatever a launcher happened to put in the environment.
	u, err := user.Current()
	if err != nil {
		fmt.Fprintf(writer, "start\tdevice ACLs\t%sUNKNOWN: %v%s\n", amber, err, reset)
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
			fmt.Fprintf(writer, "start\t%s acl\t%sUNKNOWN: %v%s\n", dev, amber, err, reset)
			continue
		}

		if hasUserACL(string(out), u.Username) {
			fmt.Fprintf(writer, "start\t%s acl\t%sOK%s\n", dev, green, reset)
			continue
		}

		fmt.Fprintf(writer, "start\t%s acl\t%sNO uaccess ACL: expect software rendering%s\n",
			dev, amber, reset)
	}
}
