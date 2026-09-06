package doctor

import (
	"os"
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCheckDevices(t *testing.T) {
	t.Parallel()

	t.Run("nothing requested is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{}
		c := checkDevices(env, "workload devices", types.HostAccess{})
		require.Equal(t, "workload devices", c.Name)
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
		require.Contains(t, c.Detail, "no host devices")
	})

	t.Run("missing device fails and names the path", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkDevices(env, "workload devices", types.HostAccess{
			Devices: []string{"/dev/ttyUSB0"},
		})
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "/dev/ttyUSB0")
	})

	t.Run("device in src:dst:perms form only stats the source", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{
			"/dev/ttyUSB0": fileInfo("ttyUSB0"),
		}}
		c := checkDevices(env, "workload devices", types.HostAccess{
			Devices: []string{"/dev/ttyUSB0:/dev/ttyUSB0:rw"},
		})
		require.Equal(t, OK, c.Status)
	})

	t.Run("usb device resolving to nothing fails and names the entry", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{usb: map[string][]string{}}
		c := checkDevices(env, "workload devices", types.HostAccess{
			USBDevices: []string{"1050:0407"},
		})
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "1050:0407")
	})

	t.Run("usb device resolving to several nodes counts as present", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{usb: map[string][]string{
			"YubiKey": {"/dev/hidraw3", "/dev/hidraw4"},
		}}
		c := checkDevices(env, "workload devices", types.HostAccess{
			USBDevices: []string{"YubiKey"},
		})
		require.Equal(t, OK, c.Status)
	})

	t.Run("usb device lookup error counts as missing rather than crashing", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{usbErr: map[string]error{
			"1050:0407": os.ErrPermission,
		}}
		c := checkDevices(env, "workload devices", types.HostAccess{
			USBDevices: []string{"1050:0407"},
		})
		require.Equal(t, Fail, c.Status)
		require.Contains(t, c.Detail, "1050:0407")
	})

	t.Run("camera requested with no video device fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{globs: map[string][]string{}}
		c := checkDevices(env, "workload devices", types.HostAccess{Camera: true})
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "camera")
	})

	t.Run("camera requested with video2 present counts as present", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{globs: map[string][]string{
			"/dev/video*": {"/dev/video2"},
		}}
		c := checkDevices(env, "workload devices", types.HostAccess{Camera: true})
		require.Equal(t, OK, c.Status)
	})

	t.Run("microphone and speakers both missing snd names audio once", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{stats: map[string]os.FileInfo{}}
		c := checkDevices(env, "workload devices", types.HostAccess{
			Microphone: true,
			Speakers:   true,
		})
		require.Equal(t, Fail, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Equal(t, 1, countOccurrences(c.Detail, "/dev/snd"))
	})

	t.Run("gpu requested with no render node warns rather than fails", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{globs: map[string][]string{}}
		c := checkDevices(env, "workload devices", types.HostAccess{Gpus: "all"})
		require.Equal(t, Warn, c.Status)
		require.NotEmpty(t, c.Fix)
		require.Contains(t, c.Detail, "gpu")
	})

	t.Run("gpu missing together with a missing device fails, not warns", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats: map[string]os.FileInfo{},
			globs: map[string][]string{},
		}
		c := checkDevices(env, "workload devices", types.HostAccess{
			Gpus:    "all",
			Devices: []string{"/dev/ttyUSB0"},
		})
		require.Equal(t, Fail, c.Status)
		require.Contains(t, c.Detail, "/dev/ttyUSB0")
		require.Contains(t, c.Detail, "gpu")
	})

	t.Run("everything present is ok", func(t *testing.T) {
		t.Parallel()

		env := &fakeEnv{
			stats: map[string]os.FileInfo{
				"/dev/ttyUSB0": fileInfo("ttyUSB0"),
				"/dev/snd":     dirInfo("snd"),
			},
			globs: map[string][]string{
				"/dev/video*":       {"/dev/video0"},
				"/dev/dri/renderD*": {"/dev/dri/renderD128"},
			},
			usb: map[string][]string{
				"1050:0407": {"/dev/hidraw3"},
			},
		}
		c := checkDevices(env, "workload devices", types.HostAccess{
			Devices:    []string{"/dev/ttyUSB0"},
			USBDevices: []string{"1050:0407"},
			Camera:     true,
			Microphone: true,
			Speakers:   true,
			Gpus:       "all",
		})
		require.Equal(t, OK, c.Status)
		require.Empty(t, c.Fix)
	})
}

func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			count++
			i += len(substr) - 1
		}
	}

	return count
}
