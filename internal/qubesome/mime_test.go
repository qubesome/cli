package qubesome

import (
	"testing"

	"github.com/qubesome/qubesome-cli/internal/types"
	"github.com/stretchr/testify/assert"
)

func Test_HandleMime(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		cfg         *types.Config
		errContains string
		workload    *WorkloadInfo
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
			name:        "error: no args",
			args:        []string{},
			errContains: "a single arg must be provided",
		},
		{
			name:        "error: two args",
			args:        []string{"/qube", "/some"},
			errContains: "a single arg must be provided",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert := assert.New(t)

			var actual *WorkloadInfo
			called := 0

			q := New()
			q.Config = tc.cfg
			q.runner = func(wi WorkloadInfo) error {
				actual = &wi
				called++
				return nil
			}

			err := q.HandleMime(tc.args)

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
