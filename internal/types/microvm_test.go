package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vm is the smallest firecracker workload that validates. Each refusal
// case below starts from it and sets one thing.
func vm() Workload {
	return Workload{
		Name:           "dev",
		Image:          "docker.io/library/debian:trixie",
		Runner:         "firecracker",
		SingleInstance: true,
	}
}

func TestValidateMicroVM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Workload)
		wantErr string
	}{
		{
			name:   "a firecracker workload with nothing else set",
			mutate: func(*Workload) {},
		},
		{
			name:    "singleInstance is false",
			mutate:  func(w *Workload) { w.SingleInstance = false },
			wantErr: "singleInstance must be true on a firecracker workload",
		},
		{
			name:    "usbDevices",
			mutate:  func(w *Workload) { w.HostAccess.USBDevices = []string{"1050:0407"} },
			wantErr: "usbDevices cannot be granted to a firecracker workload",
		},
		{
			name:    "gpus",
			mutate:  func(w *Workload) { w.HostAccess.Gpus = "all" },
			wantErr: "gpus cannot be granted to a firecracker workload",
		},
		{
			name:    "devices",
			mutate:  func(w *Workload) { w.HostAccess.Devices = []string{"/dev/kvm"} },
			wantErr: "devices cannot be granted to a firecracker workload",
		},
		{
			name:    "camera",
			mutate:  func(w *Workload) { w.HostAccess.Camera = true },
			wantErr: "camera cannot be granted to a firecracker workload",
		},
		{
			name:    "microphone",
			mutate:  func(w *Workload) { w.HostAccess.Microphone = true },
			wantErr: "microphone cannot be granted to a firecracker workload",
		},
		{
			name:    "speakers",
			mutate:  func(w *Workload) { w.HostAccess.Speakers = true },
			wantErr: "speakers cannot be granted to a firecracker workload",
		},
		{
			name:    "dbus",
			mutate:  func(w *Workload) { w.HostAccess.Dbus = true },
			wantErr: "dbus cannot be granted to a firecracker workload",
		},
		{
			name:    "bluetooth",
			mutate:  func(w *Workload) { w.HostAccess.Bluetooth = true },
			wantErr: "bluetooth cannot be granted to a firecracker workload",
		},
		{
			name:    "varRunUser",
			mutate:  func(w *Workload) { w.HostAccess.VarRunUser = true },
			wantErr: "varRunUser cannot be granted to a firecracker workload",
		},
		{
			name:    "mime",
			mutate:  func(w *Workload) { w.HostAccess.Mime = true },
			wantErr: "mime cannot be granted to a firecracker workload",
		},
		{
			name:    "capsAdd",
			mutate:  func(w *Workload) { w.HostAccess.CapsAdd = []string{"NET_ADMIN"} },
			wantErr: "capsAdd cannot be granted to a firecracker workload",
		},
		{
			name: "user",
			mutate: func(w *Workload) {
				uid := 1000
				w.User = &uid
			},
			wantErr: "user cannot be set on a firecracker workload",
		},
		{
			name:    "a read-write path",
			mutate:  func(w *Workload) { w.HostAccess.Paths = []string{"${HOME}/git:/root/git"} },
			wantErr: "cannot be granted read-write to a firecracker workload",
		},
		{
			name:   "a read-only path",
			mutate: func(w *Workload) { w.HostAccess.Paths = []string{"${HOME}/.gitconfig:/root/.gitconfig:ro"} },
		},
		{
			name: "microvm without the firecracker runner",
			mutate: func(w *Workload) {
				w.Runner = ""
				w.MicroVM = &MicroVM{VCPUs: 2}
			},
			wantErr: "microvm is set on a workload whose runner is not firecracker",
		},
		{
			name:    "attachVM together with the firecracker runner",
			mutate:  func(w *Workload) { w.AttachVM = "dev" },
			wantErr: "attachVM cannot be set on a firecracker workload",
		},
		{
			name: "attachVM on an ordinary workload",
			mutate: func(w *Workload) {
				w.Runner = ""
				w.SingleInstance = false
				w.AttachVM = "dev"
			},
		},
		{
			name: "attachVM that is not a name",
			mutate: func(w *Workload) {
				w.Runner = ""
				w.AttachVM = "../dev"
			},
			wantErr: "unsafe path",
		},
		{
			name:    "vcpus below the range",
			mutate:  func(w *Workload) { w.MicroVM = &MicroVM{VCPUs: -1} },
			wantErr: "microvm vcpus -1 is out of range",
		},
		{
			name:    "vcpus above the range",
			mutate:  func(w *Workload) { w.MicroVM = &MicroVM{VCPUs: 64} },
			wantErr: "microvm vcpus 64 is out of range",
		},
		{
			name:    "memory below the range",
			mutate:  func(w *Workload) { w.MicroVM = &MicroVM{MemoryMiB: 64} },
			wantErr: "microvm memoryMiB 64 is out of range",
		},
		{
			name:    "rootfs above the range",
			mutate:  func(w *Workload) { w.MicroVM = &MicroVM{RootfsSizeMiB: 1 << 21} },
			wantErr: "microvm rootfsSizeMiB 2097152 is out of range",
		},
		{
			name: "a data disk",
			mutate: func(w *Workload) {
				w.MicroVM = &MicroVM{Data: &MicroVMData{
					Path:  "${qubesome-data}/personal/dev.ext4",
					Mount: "/home/dev",
				}}
			},
		},
		{
			name: "a data disk with no path",
			mutate: func(w *Workload) {
				w.MicroVM = &MicroVM{Data: &MicroVMData{Mount: "/home/dev"}}
			},
			wantErr: "microvm data path cannot be empty",
		},
		{
			name: "a data disk mount that is relative",
			mutate: func(w *Workload) {
				w.MicroVM = &MicroVM{Data: &MicroVMData{Path: "/data/dev.ext4", Mount: "home/dev"}}
			},
			wantErr: "must be an absolute, clean path",
		},
		{
			name: "a data disk mount that is not clean",
			mutate: func(w *Workload) {
				w.MicroVM = &MicroVM{Data: &MicroVMData{Path: "/data/dev.ext4", Mount: "/home/../home/dev"}}
			},
			wantErr: "must be an absolute, clean path",
		},
		{
			name: "a data disk below the range",
			mutate: func(w *Workload) {
				w.MicroVM = &MicroVM{Data: &MicroVMData{
					Path: "/data/dev.ext4", Mount: "/home/dev", SizeMiB: 1,
				}}
			},
			wantErr: "microvm data sizeMiB 1 is out of range",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := vm()
			tc.mutate(&w)

			err := ValidateMicroVM(w)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// Workload.Validate is where a launch and the doctor both reach the
// refusals, so the wiring is worth its own case.
func TestWorkloadValidateRefusesAMicroVM(t *testing.T) {
	t.Parallel()

	w := vm()
	w.HostAccess.USBDevices = []string{"1050:0407"}

	err := w.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usbDevices cannot be granted to a firecracker workload")
}

func TestMicroVMWithDefaults(t *testing.T) {
	t.Parallel()

	t.Run("absent block", func(t *testing.T) {
		t.Parallel()

		var m *MicroVM
		assert.Equal(t, MicroVM{VCPUs: 2, MemoryMiB: 1024, RootfsSizeMiB: 4096}, m.WithDefaults())
	})

	t.Run("empty block", func(t *testing.T) {
		t.Parallel()

		m := &MicroVM{}
		assert.Equal(t, MicroVM{VCPUs: 2, MemoryMiB: 1024, RootfsSizeMiB: 4096}, m.WithDefaults())
	})

	t.Run("one field set", func(t *testing.T) {
		t.Parallel()

		m := &MicroVM{MemoryMiB: 8192}
		assert.Equal(t, MicroVM{VCPUs: 2, MemoryMiB: 8192, RootfsSizeMiB: 4096}, m.WithDefaults())
	})

	t.Run("a data disk gets a size", func(t *testing.T) {
		t.Parallel()

		m := &MicroVM{Data: &MicroVMData{Path: "/data/dev.ext4", Mount: "/home/dev"}}
		got := m.WithDefaults()

		require.NotNil(t, got.Data)
		assert.Equal(t, 1024, got.Data.SizeMiB)
		assert.Equal(t, 0, m.Data.SizeMiB, "the configured block must not be written back to")
	})
}

// Not parallel: captureLogs replaces the default logger, which is global.
func TestWarnIgnoredMicroVMFields(t *testing.T) {
	buf := captureLogs(t)

	WarnIgnoredMicroVMFields("dev-personal", Workload{
		X11Args:   []string{"-x"},
		NoGPUArgs: []string{"-n"},
		MimeApps:  []string{"text/plain"},
	})
	require.Empty(t, buf.String(), "an ordinary workload uses every one of them")

	WarnIgnoredMicroVMFields("dev-personal", vm())
	require.Empty(t, buf.String())

	WarnIgnoredMicroVMFields("dev-personal", Workload{
		Runner:    "firecracker",
		X11Args:   []string{"-x"},
		NoGPUArgs: []string{"-n"},
		MimeApps:  []string{"text/plain"},
	})

	out := buf.String()
	assert.Contains(t, out, "x11Args")
	assert.Contains(t, out, "noGpuArgs")
	assert.Contains(t, out, "mimeApps")
	assert.Contains(t, out, "dev-personal")
}
