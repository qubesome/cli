package cmd

import (
	"testing"

	"github.com/qubesome/cli/internal/command"
)

func init() {
	// Remove potential log noises during tests.
	DefaultLogLevel = "INFO"
}

type consoleMock = command.ConsoleMock[any]
type handlerMock = command.HandlerMock[any]

func TestExec(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		mockSetup func(*consoleMock)
	}{
		{
			name:      "empty args",
			mockSetup: func(cm *consoleMock) {},
		},
		{
			name: "insufficient args shows usage",
			args: []string{"foo"},
			mockSetup: func(cm *consoleMock) {
				cm.On("UserConfig").Return(nil)
				cm.On("Printf", usage, []interface{}{"foo"}).Return(0, nil)
				cm.On("Exit", 1)
			},
		},
		{
			name: "invalid command shows usage",
			args: []string{"foo", "bar"},
			mockSetup: func(cm *consoleMock) {
				cm.On("UserConfig").Return(nil)
				cm.On("Command", "bar").Return(false)
				cm.On("Printf", usage, []interface{}{"foo"}).Return(0, nil)
				cm.On("Exit", 1)
			},
		},
		{
			name: "valid command",
			args: []string{"foo", "bar", "of", "foo"},
			mockSetup: func(cm *consoleMock) {
				cm.On("UserConfig").Return(nil)
				cm.On("Command", "bar").Return(true)
				cm.On("RunSubCommand").Return(nil)
			},
		},
		{
			name: "valid command with options",
			args: []string{"foo", "bar", "of", "foo"},
			mockSetup: func(cm *consoleMock) {
				cm.On("UserConfig").Return(nil)
				cm.On("Command", "bar").Return(true)
				cm.On("RunSubCommand").Return(nil)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := new(consoleMock)
			ConsoleApp = m

			tc.mockSetup(m)

			Exec(tc.args)

			m.AssertExpectations(t)
		})
	}
}

type options struct{}

func WithOption1() command.Option[options] {
	return func(o *options) {
	}
}

func WithOption2() command.Option[options] {
	return func(o *options) {
	}
}
