package types

import (
	"strings"
	"testing"
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
			"runner: docker",
			Profile{
				Name:          "valid",
				Runner:        "docker",
				WindowManager: "valid",
			},
			false,
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
