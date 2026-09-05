package qubesome

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
)

func Test_HandleMime(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		cfg         *types.Config
		errContains string
		workload    *WorkloadInfo
		profile     string
	}{
		{
			name: "use default mime handler",
			args: []string{"app://foo/bar"},
			cfg: &types.Config{
				DefaultMimeHandler: &types.MimeHandler{
					Workload: "w",
					Profile:  "c",
				},
			},
			errContains: "",
			workload: &WorkloadInfo{
				Name:    "w",
				Profile: "c",
				Args:    []string{"app://foo/bar"},
				Config: &types.Config{
					DefaultMimeHandler: &types.MimeHandler{
						Workload: "w",
						Profile:  "c",
					},
				},
			},
		},
		{
			name: "use specific mime handler",
			args: []string{"app://foo/bar"},
			cfg: &types.Config{
				MimeHandlers: map[string]types.MimeHandler{
					"app": {Workload: "bar", Profile: "foo"},
				},
			},
			workload: &WorkloadInfo{
				Name:    "bar",
				Profile: "foo",
				Args:    []string{"app://foo/bar"},
				Config: &types.Config{
					MimeHandlers: map[string]types.MimeHandler{
						"app": {Workload: "bar", Profile: "foo"},
					},
				},
			},
		},
		{
			name: "prefer specific mime handler over default",
			args: []string{"app://foo/bar"},
			cfg: &types.Config{
				DefaultMimeHandler: &types.MimeHandler{
					Workload: "other",
					Profile:  "handler",
				},
				MimeHandlers: map[string]types.MimeHandler{
					"app": {Workload: "bar", Profile: "foo"},
				},
			},
			workload: &WorkloadInfo{
				Name:    "bar",
				Profile: "foo",
				Args:    []string{"app://foo/bar"},
				Config: &types.Config{
					DefaultMimeHandler: &types.MimeHandler{
						Workload: "other",
						Profile:  "handler",
					},
					MimeHandlers: map[string]types.MimeHandler{
						"app": {Workload: "bar", Profile: "foo"},
					},
				},
			},
		},
		{
			name: "error: mismatch specific handler no default mime handler",
			args: []string{"app://foo/bar"},
			cfg: &types.Config{
				MimeHandlers: map[string]types.MimeHandler{
					"foo-bar": {Workload: "foo", Profile: "bar"},
				},
			},
			errContains: "the mime type is not configured nor is a default mime",
		},
		{
			name:        "error: no specific nor default mime handler",
			args:        []string{"app://foo/bar"},
			cfg:         &types.Config{},
			errContains: "the mime type is not configured nor is a default mime",
		},
		{
			name: "error: flag instead of url",
			args: []string{"--renderer-cmd-prefix=/bin/sh"},
			cfg: &types.Config{
				DefaultMimeHandler: &types.MimeHandler{Workload: "w", Profile: "c"},
			},
			errContains: "cannot start with '-'",
		},
		{
			name: "error: single dash flag",
			args: []string{"-marionette"},
			cfg: &types.Config{
				DefaultMimeHandler: &types.MimeHandler{Workload: "w", Profile: "c"},
			},
			errContains: "cannot start with '-'",
		},
		{
			name: "error: schemeless argument that is not a file",
			args: []string{"not-a-file-nor-a-url"},
			cfg: &types.Config{
				DefaultMimeHandler: &types.MimeHandler{Workload: "w", Profile: "c"},
			},
			errContains: "neither a URL nor an existing file",
		},
		{
			name: "error: empty argument",
			args: []string{""},
			cfg: &types.Config{
				DefaultMimeHandler: &types.MimeHandler{Workload: "w", Profile: "c"},
			},
			errContains: "cannot be empty",
		},
		{
			name:        "error: no args",
			args:        []string{},
			errContains: "a single arg must be provided",
		},
		{
			name:        "error: two args",
			args:        []string{"/qube", "/some"},
			errContains: "a single arg must be provided",
		},
		{
			name: "use default mime handler with profile override",
			args: []string{"app://foo/bar"},
			cfg: &types.Config{
				DefaultMimeHandler: &types.MimeHandler{
					Workload: "w",
					Profile:  "personal",
				},
			},
			errContains: "",
			workload: &WorkloadInfo{
				Name:    "w",
				Profile: "untrusted",
				Args:    []string{"app://foo/bar"},
				Config: &types.Config{
					DefaultMimeHandler: &types.MimeHandler{
						Workload: "w",
						Profile:  "personal",
					},
				},
			},
			profile: "untrusted",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert := assert.New(t)

			var actual *WorkloadInfo
			called := 0

			q := New()
			q.runner = func(wi WorkloadInfo, _ string, _ bool) error {
				actual = &wi
				called++
				return nil
			}

			err := q.HandleMime(&WorkloadInfo{Config: tc.cfg, Profile: tc.profile}, tc.args, "")

			if tc.errContains == "" {
				assert.Nil(err)
			} else {
				assert.ErrorContains(err, tc.errContains)
			}

			if tc.workload == nil {
				assert.Equal(0, called)
			} else {
				assert.Equal(1, called)
				assert.Equal(tc.workload, actual)
			}
		})
	}
}

func Test_HandleMimeExistingFile(t *testing.T) {
	assert := assert.New(t)

	path := filepath.Join(t.TempDir(), "doc.pdf")
	assert.NoError(os.WriteFile(path, []byte("pdf"), 0o600))

	cfg := &types.Config{
		DefaultMimeHandler: &types.MimeHandler{Workload: "w", Profile: "c"},
	}

	var actual *WorkloadInfo
	q := New()
	q.runner = func(wi WorkloadInfo, _ string, _ bool) error {
		actual = &wi
		return nil
	}

	assert.NoError(q.HandleMime(&WorkloadInfo{Config: cfg}, []string{path}, ""))
	assert.NotNil(actual)
	assert.Equal([]string{path}, actual.Args)
}

func Test_HandleMimeUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	assert := assert.New(t)

	dir := filepath.Join(t.TempDir(), "locked")
	assert.NoError(os.Mkdir(dir, 0o700))
	path := filepath.Join(dir, "doc.pdf")
	assert.NoError(os.WriteFile(path, []byte("pdf"), 0o600))
	assert.NoError(os.Chmod(dir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	cfg := &types.Config{
		DefaultMimeHandler: &types.MimeHandler{Workload: "w", Profile: "c"},
	}

	called := 0
	q := New()
	q.runner = func(WorkloadInfo, string, bool) error {
		called++
		return nil
	}

	err := q.HandleMime(&WorkloadInfo{Config: cfg}, []string{path}, "")

	// A stat that fails on permissions must not be reported as the file
	// not being there.
	assert.ErrorContains(err, "failed to stat mime argument")
	assert.NotContains(err.Error(), "neither a URL nor an existing file")
	assert.Equal(0, called)
}
