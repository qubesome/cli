package images_test

import (
	"testing"

	cmd "github.com/qubesome/cli/cmd/images"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const usage = `usage:
    %[1]s images pull
    %[1]s images --profile <NAME> pull
`

type consoleMock = *command.ConsoleMock[images.Options]

func TestHandler(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(consoleMock)
		action    command.Action[images.Options]
		opts      interface{}
		err       string
	}{
		{
			name: "empty",
			mockSetup: func(cm consoleMock) {
				cm.On("Args").Return([]string{})
				cm.On("Usage", usage)
			},
			opts: &images.Options{},
		},
		{
			name: "invalid subcommand",
			mockSetup: func(cm consoleMock) {
				cm.On("Args").Return([]string{"foo"})
				cm.On("Usage", usage)
			},
			opts: &images.Options{},
		},
		{
			name: "no user config",
			mockSetup: func(cm consoleMock) {
				cm.On("Args").Return([]string{"pull"})
				cm.On("UserConfig").Return(nil)
			},
			err:  "no config found",
			opts: &images.Options{},
		},
		{
			name: "no profile config",
			mockSetup: func(cm consoleMock) {
				cm.On("Args").Return([]string{"--profile", "foo", "pull"})
				cm.On("UserConfig").Return(nil)
				cm.On("ProfileConfig", "foo").Return(nil)
			},
			err:  "no config found",
			opts: &images.Options{},
		},
		{
			name: "load profile config",
			mockSetup: func(cm consoleMock) {
				cm.On("Args").Return([]string{"--profile", "bar", "pull"})
				cm.On("UserConfig").Return(nil)
				cm.On("ProfileConfig", "bar").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"bar": {Name: "bar"},
					},
				})
			},
			action: cmd.New().(command.Action[images.Options]),
			opts: &images.Options{
				Config: &types.Config{
					Profiles: map[string]*types.Profile{
						"bar": {Name: "bar"},
					},
				},
			},
		},
		{
			name: "images pull",
			mockSetup: func(cm consoleMock) {
				cm.On("Args").Return([]string{"pull"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				})
			},
			action: cmd.New().(command.Action[images.Options]),
			opts: &images.Options{
				Config: &types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := new(command.ConsoleMock[images.Options])
			hm := command.NewHandlerMock(cmd.New().Handle)

			tc.mockSetup(cm)

			action, opts, err := hm.Handle(cm)
			if tc.err == "" {
				require.NoError(t, err)

				o := &images.Options{}
				for _, opt := range opts {
					opt(o)
				}

				assert.Equal(t, tc.action, action)
				assert.Equal(t, tc.opts, o)
			} else {
				require.ErrorContains(t, err, tc.err)
			}

			cm.AssertExpectations(t)
			hm.AssertExpectations(t)
		})
	}
}
