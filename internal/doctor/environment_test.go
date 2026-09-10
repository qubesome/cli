package doctor

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/gateway"
	"github.com/stretchr/testify/require"
)

type fakeEnv struct {
	paths  map[string]string
	stats  map[string]os.FileInfo
	env    map[string]string
	output map[string]fakeOutput
	globs  map[string][]string
	usb    map[string][]string
	usbErr map[string]error
	mounts map[string]string
	links  map[string]string
	images map[string]bool
	alive  map[string]bool
	uid    string

	// gatewayReady is what the control client would have answered. A nil
	// error is a ready gateway, which is also the zero value, so only a
	// test that wants a failure has to set it.
	gatewayReady error
	log          gateway.LogSummary
	logErr       error
}

type fakeOutput struct {
	out []byte
	err error
}

func (f *fakeEnv) LookPath(file string) (string, error) {
	if p, ok := f.paths[file]; ok {
		return p, nil
	}

	return "", exec.ErrNotFound
}

func (f *fakeEnv) Stat(path string) (os.FileInfo, error) {
	if fi, ok := f.stats[path]; ok {
		return fi, nil
	}

	return nil, os.ErrNotExist
}

// Lstat answers from the same table as Stat, since no test needs a
// symlink to be judged differently from its target.
func (f *fakeEnv) Lstat(path string) (os.FileInfo, error) {
	return f.Stat(path)
}

func (f *fakeEnv) Readlink(path string) (string, error) {
	if target, ok := f.links[path]; ok {
		return target, nil
	}

	return "", os.ErrNotExist
}

func (f *fakeEnv) Getenv(key string) string {
	return f.env[key]
}

func (f *fakeEnv) UID() string {
	return f.uid
}

func (f *fakeEnv) Output(name string, args ...string) ([]byte, error) {
	key := name
	if len(args) > 0 {
		key = name + " " + strings.Join(args, " ")
	}

	if o, ok := f.output[key]; ok {
		return o.out, o.err
	}

	return nil, exec.ErrNotFound
}

func (f *fakeEnv) Glob(pattern string) ([]string, error) {
	return f.globs[pattern], nil
}

// USBNamed resolves each requested name against f.usb and f.usbErr, keyed
// by the single name it was asked about, since checkDevices always asks
// one name at a time.
func (f *fakeEnv) USBNamed(names []string) ([]string, error) {
	var matches []string

	for _, name := range names {
		if err, ok := f.usbErr[name]; ok {
			return nil, err
		}

		matches = append(matches, f.usb[name]...)
	}

	return matches, nil
}

// Mounted answers from a device to mountpoint table, the way
// /proc/mounts does.
func (f *fakeEnv) Mounted(device, mount string) (bool, error) {
	return f.mounts[device] == mount, nil
}

func (f *fakeEnv) ImageInStore(ref string) bool {
	return f.images[ref]
}

// SandboxAlive answers from a table keyed by state file path, so a test
// that wants a profile up records the path that profile writes.
func (f *fakeEnv) SandboxAlive(path string) bool {
	return f.alive[path]
}

func (f *fakeEnv) GatewayReady() error {
	return f.gatewayReady
}

// GatewayLog answers with a canned summary, so a test can drive a check
// on what a gateway log said without writing one.
func (f *fakeEnv) GatewayLog() (gateway.LogSummary, error) {
	return f.log, f.logErr
}

type fakeFileInfo struct {
	name   string
	isDir  bool
	isSock bool
	size   int64
}

func (f fakeFileInfo) Name() string { return f.name }
func (f fakeFileInfo) Size() int64  { return f.size }
func (f fakeFileInfo) Mode() os.FileMode {
	switch {
	case f.isDir:
		return os.ModeDir | 0o755
	case f.isSock:
		return os.ModeSocket | 0o755
	default:
		return 0o644
	}
}
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.isDir }
func (f fakeFileInfo) Sys() any           { return nil }

func dirInfo(name string) os.FileInfo {
	return fakeFileInfo{name: name, isDir: true}
}

func fileInfo(name string) os.FileInfo {
	return fakeFileInfo{name: name, isDir: false}
}

// socketInfo is used by the profile checks that stat a unix socket path,
// since a regular file left where a socket should be is itself a finding.
func socketInfo(name string) os.FileInfo {
	return fakeFileInfo{name: name, isSock: true}
}

// sizedFileInfo is used by the profile cookie checks, which treat a
// zero-sized cookie file the same as a missing one.
func sizedFileInfo(name string, size int64) os.FileInfo {
	return fakeFileInfo{name: name, size: size}
}

type exitError struct {
	code int
}

func (e exitError) Error() string { return "exit status" }

func TestCheckSandboxTools(t *testing.T) {
	t.Parallel()

	t.Run("all present is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{paths: map[string]string{
			files.BwrapBinary:  files.BwrapBinary,
			files.SkopeoBinary: files.SkopeoBinary,
			files.UmociBinary:  files.UmociBinary,
		}}
		c := checkSandboxTools(env)
		require.Equal(t, "sandbox tools", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, files.BwrapBinary)
	})

	t.Run("every missing tool is named, not just the first", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{paths: map[string]string{files.BwrapBinary: files.BwrapBinary}}
		c := checkSandboxTools(env)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, files.SkopeoBinary)
		require.Contains(t, c.Detail, files.UmociBinary)
		require.NotContains(t, c.Detail, files.BwrapBinary)
	})

	t.Run("nothing installed fails", func(t *testing.T) {
		t.Parallel()

		c := checkSandboxTools(&fakeEnv{paths: map[string]string{}})
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
	})
}

func TestCheckResolution(t *testing.T) {
	t.Parallel()

	t.Run("neither binary present fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{paths: map[string]string{}}
		c := checkResolution(env)
		require.Equal(t, "screen resolution", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, strings.ToLower(c.Fix), "xrandr")
	})

	t.Run("xrandr present and working is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			paths: map[string]string{files.XrandrBinary: files.XrandrBinary},
			output: map[string]fakeOutput{
				files.XrandrBinary: {out: []byte("Screen 0: minimum 320 x 200")},
			},
		}
		c := checkResolution(env)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, files.XrandrBinary)
	})

	t.Run("xrandr present but failing fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			paths: map[string]string{files.XrandrBinary: files.XrandrBinary},
			output: map[string]fakeOutput{
				files.XrandrBinary: {out: []byte("Can't open display"), err: exitError{1}},
			},
		}
		c := checkResolution(env)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "Can't open display")
	})

	t.Run("xrandr absent but wlr-randr present is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			paths: map[string]string{files.WlrRandrBinary: files.WlrRandrBinary},
			output: map[string]fakeOutput{
				files.WlrRandrBinary: {out: []byte("eDP-1")},
			},
		}
		c := checkResolution(env)
		require.Equal(t, OK, c.Status)
		require.Contains(t, c.Detail, files.WlrRandrBinary)
	})
}

func TestCheckHostDisplay(t *testing.T) {
	t.Parallel()

	t.Run("unset fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{env: map[string]string{}}
		c := checkHostDisplay(env)
		require.Equal(t, "host display", c.Name)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("malformed fails and quotes the value", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{env: map[string]string{"DISPLAY": "bogus"}}
		c := checkHostDisplay(env)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "bogus")
	})

	t.Run("set but socket missing fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			env:   map[string]string{"DISPLAY": ":0"},
			stats: map[string]os.FileInfo{},
		}
		c := checkHostDisplay(env)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "/tmp/.X11-unix/X0")
	})

	t.Run("set and socket present is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			env: map[string]string{"DISPLAY": ":0"},
			stats: map[string]os.FileInfo{
				"/tmp/.X11-unix/X0": fileInfo("X0"),
			},
		}
		c := checkHostDisplay(env)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, ":0")
	})

	t.Run("colon dot form parses to socket X0", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			env: map[string]string{"DISPLAY": ":0.0"},
			stats: map[string]os.FileInfo{
				"/tmp/.X11-unix/X0": fileInfo("X0"),
			},
		}
		c := checkHostDisplay(env)
		require.Equal(t, OK, c.Status)
	})
}

func TestCheckRenderNode(t *testing.T) {
	t.Parallel()

	t.Run("dri missing warns", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkRenderNode(env)
		require.Equal(t, "gpu render node", c.Name)
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("renderD128 missing warns", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats: map[string]os.FileInfo{
				"/dev/dri": dirInfo("dri"),
			},
		}
		c := checkRenderNode(env)
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("both present is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats: map[string]os.FileInfo{
				"/dev/dri":            dirInfo("dri"),
				"/dev/dri/renderD128": fileInfo("renderD128"),
			},
		}
		c := checkRenderNode(env)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, "renderD128")
	})
}

func TestCheckDbus(t *testing.T) {
	t.Parallel()

	t.Run("address in the environment is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{env: map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus"}}
		c := checkDbus(env)
		require.Equal(t, "desktop notifications", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})

	t.Run("autolaunch address is not an address", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			env:   map[string]string{"DBUS_SESSION_BUS_ADDRESS": "autolaunch:"},
			uid:   "1000",
			paths: map[string]string{},
		}
		c := checkDbus(env)
		require.Equal(t, Warn, c.Status)
	})

	t.Run("socket under the runtime dir is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			uid:   "1000",
			stats: map[string]os.FileInfo{"/run/user/1000/bus": &fakeFileInfo{name: "bus", isSock: true}},
		}
		c := checkDbus(env)
		require.Equal(t, OK, c.Status)
		require.Contains(t, c.Detail, "/run/user/1000/bus")
	})

	t.Run("dbus-session file under the runtime dir is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			uid:   "1000",
			stats: map[string]os.FileInfo{"/run/user/1000/dbus-session": &fakeFileInfo{name: "dbus-session"}},
		}
		c := checkDbus(env)
		require.Equal(t, OK, c.Status)
		require.Contains(t, c.Detail, "/run/user/1000/dbus-session")
	})

	t.Run("no bus but dbus-launch present warns about an autolaunched bus", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			uid:   "1000",
			paths: map[string]string{dbusLaunchBinary: "/usr/bin/" + dbusLaunchBinary},
		}
		c := checkDbus(env)
		require.Equal(t, Warn, c.Status)
		require.Contains(t, c.Detail, "started on demand")
		require.NotEmpty(t, c.Fix)
	})

	t.Run("no bus and no dbus-launch warns", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{uid: "1000", paths: map[string]string{}}
		c := checkDbus(env)
		require.Equal(t, Warn, c.Status)
		require.Contains(t, c.Detail, "none can be started")
		require.NotEmpty(t, c.Fix)
	})
}

func TestCheckQubesomeDir(t *testing.T) {
	t.Parallel()

	t.Run("missing warns", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkQubesomeDir(env)
		require.Equal(t, "qubesome directory", c.Name)
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("present as file fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats: map[string]os.FileInfo{
				files.QubesomeDir(): fileInfo("qubesome"),
			},
		}
		c := checkQubesomeDir(env)
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
	})

	t.Run("present as dir is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats: map[string]os.FileInfo{
				files.QubesomeDir(): dirInfo("qubesome"),
			},
		}
		c := checkQubesomeDir(env)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, files.QubesomeDir())
	})
}

func TestEnvironment(t *testing.T) {
	t.Parallel()

	env := &fakeEnv{
		paths: map[string]string{
			files.BwrapBinary:  files.BwrapBinary,
			files.SkopeoBinary: files.SkopeoBinary,
			files.UmociBinary:  files.UmociBinary,
			files.XrandrBinary: files.XrandrBinary,
		},
		uid: "1000",
		env: map[string]string{"DISPLAY": ":0"},
		stats: map[string]os.FileInfo{
			"/run/user/1000/bus":  fileInfo("bus"),
			"/tmp/.X11-unix/X0":   fileInfo("X0"),
			"/dev/dri":            dirInfo("dri"),
			"/dev/dri/renderD128": fileInfo("renderD128"),
			files.QubesomeDir():   dirInfo("qubesome"),
		},
		output: map[string]fakeOutput{
			files.XrandrBinary: {out: []byte("Screen 0")},
		},
	}

	checks := Environment(env)
	require.Len(t, checks, 6)

	names := []string{
		"sandbox tools",
		"screen resolution",
		"host display",
		"gpu render node",
		"desktop notifications",
		"qubesome directory",
	}
	for i, c := range checks {
		require.Equal(t, names[i], c.Name)
		require.NotEmpty(t, c.Name)
		require.NotEmpty(t, c.Detail)
	}
}
