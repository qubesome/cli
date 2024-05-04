package start_test

import (
	"testing"

	cmd "github.com/qubesome/cli/cmd/start"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/profiles"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const usage = `usage:
    %[1]s start <profile>
    %[1]s start -git=https://github.com/qubesome/dotfiles-example -path / <profile>
`

type consoleMock = *command.ConsoleMock[profiles.Options]
type handlerMock = *command.HandlerMock[profiles.Options]

func TestHandler(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(consoleMock, handlerMock)
		action    command.Action[profiles.Options]
		opts      interface{}
		err       string
	}{
		{
			name: "empty",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{})
				cm.On("Usage", usage)
			},
			opts: &profiles.Options{},
		},
		{
			name: "no config",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"foo"})
				cm.On("UserConfig").Return(nil)
			},
			action: cmd.New().(command.Action[profiles.Options]),
			opts: &profiles.Options{
				Profile: "foo",
			},
		},
		{
			name: "foo",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"foo"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				})
			},
			action: cmd.New().(command.Action[profiles.Options]),
			opts: &profiles.Options{
				Profile: "foo",
				Config: &types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				},
			},
		},
		{
			name: "foo from git",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"-git", "places", "foo"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				})
			},
			action: cmd.New().(command.Action[profiles.Options]),
			opts: &profiles.Options{
				Profile: "foo",
				GitUrl:  "places",
				Config: &types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				},
			},
		},
		{
			name: "foo from git+path",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"-git", "places", "-path", "/bar", "foo"})
				cm.On("UserConfig").Return(&types.Config{
					Profiles: map[string]*types.Profile{
						"foo": {Name: "foo"},
					},
				})
			},
			action: cmd.New().(command.Action[profiles.Options]),
			opts: &profiles.Options{
				Profile: "foo",
				GitUrl:  "places",
				Path:    "/bar",
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
			cm := new(command.ConsoleMock[profiles.Options])
			hm := command.NewHandlerMock(cmd.New().Handle)

			tc.mockSetup(cm, hm)

			action, opts, err := hm.Handle(cm)
			if tc.err == "" {
				require.NoError(t, err)

				assert.Equal(t, tc.action, action)

				o := &profiles.Options{}
				for _, opt := range opts {
					opt(o)
				}

				assert.Equal(t, tc.opts, o)
			} else {
				require.ErrorContains(t, err, tc.err)
			}

			cm.AssertExpectations(t)
			hm.AssertExpectations(t)
		})
	}
}
