package deps_test

import (
	"testing"

	"github.com/qubesome/cli/cmd/deps"
	"github.com/qubesome/cli/internal/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const usage = `usage: %[1]s deps show
`

type consoleMock = command.ConsoleMock[any]
type handlerMock = command.HandlerMock[any]

func TestCommand(t *testing.T) {
	tests := []struct { //nolint
		name      string
		mockSetup func(*consoleMock, *handlerMock)
		action    command.Action[any]
		opts      []command.Option[any]
		err       *error
	}{
		{
			name: "empty",
			mockSetup: func(cm *consoleMock, hm *handlerMock) {
				cm.On("Args").Return([]string{})
				cm.On("Usage", usage)
			},
		},
		{
			name: "show",
			mockSetup: func(cm *consoleMock, hm *handlerMock) {
				cm.On("Args").Return([]string{"show"})
				cm.On("Run", []command.Option[any](nil)).Return(nil)
			},
			action: deps.New().(command.Action[any]),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := new(consoleMock)
			hm := command.NewHandlerMock[any](deps.New().Handle)

			tc.mockSetup(cm, hm)

			action, opts, err := hm.Handle(cm)
			if tc.err == nil {
				require.NoError(t, err)

				assert.Equal(t, tc.action, action)
				assert.Equal(t, tc.opts, opts)
			} else {
				require.ErrorAs(t, err, *tc.err)
			}

			hm.AssertExpectations(t)
		})
	}
}
