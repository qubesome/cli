package xdg_test

import (
	"testing"

	cmd "github.com/qubesome/cli/cmd/xdg"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const usage = `usage:
    %[1]s xdg open https://google.com
    %[1]s xdg --profile personal open https://google.com
`

type consoleMock = *command.ConsoleMock[qubesome.Options]
type handlerMock = *command.HandlerMock[qubesome.Options]

func TestHandler(t *testing.T) {
	tests := []struct {
		name      string
		mockSetup func(consoleMock, handlerMock)
		action    command.Action[qubesome.Options]
		opts      interface{}
		err       string
	}{
		{
			name: "empty",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{})
				cm.On("Usage", usage)
			},
			opts: &qubesome.Options{},
		},
		{
			name: "no config",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"open", "foo"})
				cm.On("UserConfig").Return(nil)
			},
			err: "no config found",
		},
		{
			name: "no profile config",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--profile", "bar", "open", "foo"})
				cm.On("UserConfig").Return(nil)
				cm.On("ProfileConfig", "bar").Return(nil)
			},
			err: "no config found",
		},
		{
			name: "https://url",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"open", "https://url"})
				cm.On("UserConfig").Return(&types.Config{})
			},
			action: cmd.New().(command.Action[qubesome.Options]),
			opts: &qubesome.Options{
				ExtraArgs: []string{"https://url"},
				Config:    &types.Config{},
			},
		},
		{
			name: "profile + https://url",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--profile", "bar", "open", "https://url"})
				cm.On("UserConfig").Return(&types.Config{})
			},
			action: cmd.New().(command.Action[qubesome.Options]),
			opts: &qubesome.Options{
				Profile:   "bar",
				ExtraArgs: []string{"https://url"},
				Config:    &types.Config{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := new(command.ConsoleMock[qubesome.Options])
			hm := command.NewHandlerMock[qubesome.Options](cmd.New().Handle)

			tc.mockSetup(cm, hm)

			action, opts, err := hm.Handle(cm)
			if tc.err == "" {
				require.NoError(t, err)

				assert.Equal(t, tc.action, action)

				o := &qubesome.Options{}
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
