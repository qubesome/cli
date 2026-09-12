// Package tz carries the host's timezone into a sandbox.
package tz

import (
	"log/slog"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// hostLocaltime is where a host records the zone it is set to.
const hostLocaltime = "/etc/localtime"

// sandboxZoneinfo is the directory a C library searches for the zone TZ
// names, and it is the same one on every distribution whatever the host
// keeps its own database under.
const sandboxZoneinfo = "/usr/share/zoneinfo"

// unnamedZone is where a host zone file that names no zone is shared. No
// timezone database ships a file called localtime, so this destination
// cannot land on top of a real zone.
const unnamedZone = sandboxZoneinfo + "/localtime"

// namePattern is an IANA zone name: slash separated components of the
// characters tzdata uses in its file names, such as Europe/London,
// America/Argentina/Buenos_Aires, Etc/GMT+1 or plain UTC.
//
// A path that does not match names no zone. Passing one to TZ would be
// worse than not naming a zone at all, because a C library that cannot
// read TZ as a zone name reads it as a POSIX rule instead, and every
// rule it fails to parse leaves the sandbox on UTC without saying so.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_+-]+(?:/[A-Za-z0-9_+-]+)*$`)

// Zone is the host's timezone in the form a sandbox needs it.
type Zone struct {
	// HostPath is the zone file on the host, with every symlink already
	// resolved. It is empty when the host has no timezone to give, which
	// is the only field a caller needs to test.
	HostPath string

	// SandboxPath is where HostPath is shared inside the sandbox. It is
	// never /etc/localtime: an image almost always ships that as a
	// symlink, and bubblewrap 0.12.0 refuses to mount on one. Releases
	// before it followed the link and mounted on its target, which is
	// why sharing /etc/localtime worked until 0.12.0 landed.
	SandboxPath string

	// TZ is the value of the TZ environment variable, naming the zone
	// that SandboxPath holds.
	//
	// This is what actually gives the sandbox the host's time, since
	// /etc/localtime inside it still comes from the image. Every C
	// library reads TZ ahead of /etc/localtime, and so do the runtimes
	// that resolve a zone themselves rather than through one.
	TZ string
}

// Host returns the host's timezone, and the zero Zone when the host has
// none to give.
func Host() Zone {
	return zoneOf(hostLocaltime)
}

func zoneOf(localtime string) Zone {
	// The link is followed here rather than shared as it is, so that a
	// host naming its zone through one of the database's own aliases,
	// GB for Europe/London, shares the file under the name every image
	// has it under.
	file, err := filepath.EvalSymlinks(localtime)
	if err != nil {
		slog.Debug("no host timezone to share", "path", localtime, "error", err)
		return Zone{}
	}

	name, ok := nameOf(file)
	if !ok {
		// A host that copied its zone file into place instead of
		// linking to one has a timezone but no name for it. TZ takes
		// the file directly, which loses only the name: the offsets and
		// abbreviations all come out of the file either way.
		return Zone{HostPath: file, SandboxPath: unnamedZone, TZ: ":" + unnamedZone}
	}

	return Zone{HostPath: file, SandboxPath: path.Join(sandboxZoneinfo, name), TZ: name}
}

// nameOf reads the zone name out of a resolved zone file's path, which
// is whatever follows the timezone database directory.
func nameOf(file string) (string, bool) {
	const database = "/zoneinfo/"

	i := strings.LastIndex(file, database)
	if i < 0 {
		return "", false
	}

	name := file[i+len(database):]
	if !namePattern.MatchString(name) {
		return "", false
	}

	return name, true
}
