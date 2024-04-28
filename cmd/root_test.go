package cmd

import (
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/mock"
)

type ConsoleMock struct {
	mock.Mock
}

func (m *ConsoleMock) Exit(code int) {
	m.Called(code)
}

func (m *ConsoleMock) Printf(format string, a ...any) (int, error) {
	args := m.Called(format, a)
	return args.Int(0), args.Error(1)
}

func (m *ConsoleMock) Command(name string) (func([]string, *types.Config) error, bool) {
	args := m.Called(name)
	return args.Get(0).(func([]string, *types.Config) error), args.Bool(1)
}

func (m *ConsoleMock) Fake(a []string, c *types.Config) error {
	args := m.Called(a, c)
	return args.Error(0)
}

func TestExec(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		mockSetup func(*ConsoleMock)
	}{
		{name: "empty args"},
		{
			name: "insufficient args shows usage",
			args: []string{"foo"},
			mockSetup: func(cm *ConsoleMock) {
				cm.On("Printf", usage, []interface{}{"foo"}).Return(0, nil)
				cm.On("Exit", 1)
			},
		},
		{
			name: "invalid command shows usage",
			args: []string{"foo", "bar"},
			mockSetup: func(cm *ConsoleMock) {
				cm.On("Command", "bar").Return(cm.Fake, false)
				cm.On("Printf", usage, []interface{}{"foo"}).Return(0, nil)
				cm.On("Exit", 1)
			},
		},
		{
			name: "valid command",
			args: []string{"foo", "bar", "of", "foo"},
			mockSetup: func(cm *ConsoleMock) {
				cm.On("Command", "bar").Return(cm.Fake, true)

				var cfg *types.Config
				cm.On("Fake", []string{"of", "foo"}, cfg).Return(nil)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := new(ConsoleMock)
			ConsoleApp = m

			if tc.mockSetup != nil {
				tc.mockSetup(m)
			}

			Exec(tc.args)

			m.AssertExpectations(t)
		})
	}
}
