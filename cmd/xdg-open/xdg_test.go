package xdg_test

import (
	"testing"

	cmd "github.com/qubesome/cli/cmd/xdg-open"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const usage = `usage:
    %[1]s xdg-open https://google.com
    %[1]s xdg-open --profile personal https://google.com
`

type consoleMock = *command.ConsoleMock[qubesome.Options]
type handlerMock = *command.HandlerMock[qubesome.Options]

func TestHandler(t *testing.T) {
	tests := []struct { //nolint
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
				cm.On("Args").Return([]string{"foo"})
				cm.On("UserConfig").Return(nil)
			},
			action: cmd.New().(command.Action[qubesome.Options]),
			opts: &qubesome.Options{
				ExtraArgs: []string{"foo"},
				Config:    nil,
			},
		},
		{
			name: "no profile config",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"--profile", "bar", "foo"})
				cm.On("ProfileConfig", "bar").Return(nil)
				cm.On("UserConfig").Return(nil)
			},
			action: cmd.New().(command.Action[qubesome.Options]),
			opts: &qubesome.Options{
				ExtraArgs: []string{"foo"},
				Profile:   "bar",
				Config:    nil,
			},
		},
		{
			name: "https://url",
			mockSetup: func(cm consoleMock, hm handlerMock) {
				cm.On("Args").Return([]string{"https://url"})
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
				cm.On("Args").Return([]string{"--profile", "bar", "https://url"})
				cm.On("ProfileConfig", "bar").Return(nil)
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
