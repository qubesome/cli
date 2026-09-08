package types

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfileValidate(t *testing.T) {
	tests := []struct {
		name    string
		profile Profile
		wantErr bool
	}{
		{
			"name: valid",
			Profile{
				Name:          "FOO-bar-321",
				WindowManager: "valid",
			},
			false,
		},
		{
			"name: valid long",
			Profile{
				Name:          strings.Repeat("a", 50),
				WindowManager: "valid",
			},
			false,
		},
		{
			"name: invalid space",
			Profile{
				Name:          "in valid",
				WindowManager: "valid",
			},
			true,
		},
		{
			"name: invalid '",
			Profile{
				Name:          "in'valid",
				WindowManager: "valid",
			},
			true,
		},
		{
			"name: invalid \"",
			Profile{
				Name:          "in\"valid",
				WindowManager: "valid",
			},
			true,
		},
		{
			"name: invalid empty",
			Profile{
				Name:          "",
				WindowManager: "valid",
			},
			true,
		},
		{
			"name: invalid too long",
			Profile{
				Name:          strings.Repeat("a", 51),
				WindowManager: "valid",
			},
			true,
		},
		{
			"timezone: valid",
			Profile{
				Name:          "valid",
				Timezone:      "Europe/London",
				WindowManager: "valid",
			},
			false,
		},
		{
			"timezone: invalid space",
			Profile{
				Name:          "valid",
				Timezone:      "Europe London",
				WindowManager: "valid",
			},
			true,
		},
		{
			"image: valid",
			Profile{
				Name:          "valid",
				Image:         "test/abc:v1.2.3",
				WindowManager: "valid",
			},
			false,
		},
		{
			"image: valid",
			Profile{
				Name:          "valid",
				Image:         "foo.bar/abc/cba:v1.2.3",
				WindowManager: "valid",
			},
			false,
		},
		{
			"image: valid empty",
			Profile{
				Name:          "valid",
				WindowManager: "valid",
			},
			false,
		},
		{
			"dns: valid empty",
			Profile{
				Name:          "valid",
				DNS:           "",
				WindowManager: "valid",
			},
			false,
		},
		{
			"dns: valid empty",
			Profile{
				Name:          "valid",
				DNS:           "1.1.1.1",
				WindowManager: "valid",
			},
			false,
		},
		{
			"windowManager: valid",
			Profile{
				Name:          "valid",
				WindowManager: "exec awesome",
			},
			false,
		},
		{
			"windowManager: invalid empty",
			Profile{
				Name:          "valid",
				WindowManager: "",
			},
			true,
		},
		{
			"runner: removed docker",
			Profile{
				Name:          "valid",
				Runner:        "docker",
				WindowManager: "valid",
			},
			true,
		},
		{
			"runner: removed podman",
			Profile{
				Name:          "valid",
				Runner:        "podman",
				WindowManager: "valid",
			},
			true,
		},
		{
			"runner: firecracker",
			Profile{
				Name:          "valid",
				Runner:        "firecracker",
				WindowManager: "valid",
			},
			false,
		},
		{
			"runner: empty",
			Profile{
				Name:          "valid",
				Runner:        "",
				WindowManager: "valid",
			},
			false,
		},
		{
			"runner: invalid",
			Profile{
				Name:          "valid",
				Runner:        "foo",
				WindowManager: "valid",
			},
			true,
		},
		{
			"xephyrArgs: valid empty",
			Profile{
				Name:          "valid",
				XephyrArgs:    "",
				WindowManager: "valid",
			},
			false,
		},
		{
			"externalDrives: valid",
			Profile{
				Name:          "valid",
				WindowManager: "valid",
				ExternalDrives: []string{
					"label:/host/dev/path:/mount/path",
					"label-with-dashes:/ho-st/de-v/pa-th:/mou-nt/pa-th",
				},
			},
			false,
		},
		{
			"externalDrives: invalid missing label",
			Profile{
				Name:          "valid",
				WindowManager: "valid",
				ExternalDrives: []string{
					"/host/dev/path:/mount/path",
				},
			},
			true,
		},
		{
			"externalDrives: invalid missing mount",
			Profile{
				Name:          "valid",
				WindowManager: "valid",
				ExternalDrives: []string{
					"label-with-dashes:/ho-st/de-v/pa-th",
				},
			},
			true,
		},
		{
			"paths: valid paths",
			Profile{
				Name:          "valid",
				WindowManager: "valid",
				Paths: []string{
					"/host/path:/container/path",
					"/host/path:/container/path:ro",
					"${FOO-bar}/host/path:/container/path:ro",
				},
			},
			false,
		},
		{
			"paths: invalid rel host path",
			Profile{
				Name:          "valid",
				WindowManager: "valid",
				Paths: []string{
					"rel/path:/abs/path",
				},
			},
			true,
		},
		{
			"paths: invalid rel container path",
			Profile{
				Name:          "valid",
				WindowManager: "valid",
				Paths: []string{
					"/abs/path:rel/path",
				},
			},
			true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.profile.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected an error but got nil: %+v", tc.profile)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("did not expect an error but got %v: %+v", err, tc.profile)
			}
		})
	}
}

// A profile naming a removed runner is told so too, in the same words the
// workload gets, so the two do not read as different problems.
func TestProfileValidateReportsARemovedRunner(t *testing.T) {
	t.Parallel()

	for _, runner := range []string{"docker", "podman"} {
		p := Profile{Name: "valid", WindowManager: "valid", Runner: runner}

		err := p.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has been removed")
		assert.Contains(t, err.Error(), runner)
	}
}

// A profile dns is still accepted. Nothing reads it, and it warns at
// start rather than failing a config that has always loaded.
func TestProfileValidateAcceptsDNS(t *testing.T) {
	t.Parallel()

	p := Profile{Name: "valid", WindowManager: "valid", DNS: "1.1.1.1"}

	require.NoError(t, p.Validate())
}

// captureLogs redirects the default logger for the duration of the test
// and returns what was written to it. It replaces a package level logger,
// so a test using it cannot run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return buf
}

// A named network is kept and reported, never refused. The message has to
// name the field and say the value does nothing yet, since the config
// looks like it grants a network and the sandbox gets loopback only.
func TestWarnIgnoredNetwork(t *testing.T) {
	buf := captureLogs(t)

	for _, network := range []string{"", "none", "host"} {
		WarnIgnoredNetwork("chrome-work", network)
	}
	require.NotContains(t, buf.String(), "hostAccess.network")

	WarnIgnoredNetwork("chrome-work", "qubesome")

	out := buf.String()
	assert.Contains(t, out, "hostAccess.network")
	assert.Contains(t, out, "ignored")
	assert.Contains(t, out, "qubesome")
	assert.Contains(t, out, "chrome-work")
}

func TestWarnIgnoredDNS(t *testing.T) {
	buf := captureLogs(t)

	WarnIgnoredDNS("work", "")
	require.NotContains(t, buf.String(), "profile dns")

	WarnIgnoredDNS("work", "1.1.1.1")

	out := buf.String()
	assert.Contains(t, out, "dns")
	assert.Contains(t, out, "ignored")
	assert.Contains(t, out, "1.1.1.1")
}
