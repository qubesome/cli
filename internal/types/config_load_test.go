package types

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "valid config",
			content: `profiles:
  work:
    display: 1
    hostAccess:
      camera: true
`,
			wantErr: false,
		},
		{
			name: "unknown field",
			content: `profiles:
  work:
    display: 1
    notAField: true
`,
			wantErr: true,
		},
		{
			name: "malformed yaml",
			content: `profiles:
  work:
   display: 1
      camera: true
`,
			wantErr: true,
		},
		{
			name:    "empty document",
			content: "",
			wantErr: true,
		},
		{
			name: "wrong field type",
			content: `profiles:
  work:
    display: not-a-number
`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "qubesome.config")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			cfg, err := LoadConfig(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got config: %+v", cfg)
				}
				if cfg != nil {
					t.Errorf("expected nil config on error, got: %+v", cfg)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := cfg.Profile("work"); !ok {
				t.Error("expected profile 'work' to be loaded")
			}
			if cfg.Profiles["work"].Name != "work" {
				t.Errorf("expected profile name to be populated, got %q", cfg.Profiles["work"].Name)
			}
		})
	}
}

// A null document decodes into a Config with no profiles. It must not set
// the returned pointer to nil, which is what decoding into a **Config did.
func TestLoadConfigNullDocument(t *testing.T) {
	t.Parallel()

	for _, doc := range []string{"null\n", "~\n"} {
		path := filepath.Join(t.TempDir(), "qubesome.config")
		if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("doc %q: unexpected error: %v", doc, err)
		}
		if cfg == nil {
			t.Fatalf("doc %q: config must not be nil", doc)
		}
		if len(cfg.Profiles) != 0 {
			t.Errorf("doc %q: expected no profiles, got %v", doc, cfg.Profiles)
		}
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatalf("expected error, got config: %+v", cfg)
	}
	if cfg != nil {
		t.Errorf("expected nil config on error, got: %+v", cfg)
	}
}
