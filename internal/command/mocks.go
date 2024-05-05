package command

import (
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/mock"
)

func NewHandlerMock[T any](f func(a App) (Action[T], []Option[T], error)) *HandlerMock[T] {
	return &HandlerMock[T]{
		handle: f,
	}
}

type HandlerMock[T any] struct {
	handle func(a App) (Action[T], []Option[T], error)
	mock.Mock
}

func (m *HandlerMock[T]) Handle(a App) (Action[T], []Option[T], error) {
	return m.handle(a)
}

func (m *HandlerMock[T]) Run(opts ...Option[T]) error {
	args := m.Called(opts)
	return args.Error(0)
}

type ConsoleMock[T any] struct {
	mock.Mock
}

func (m *ConsoleMock[T]) ExecName() string {
	args := m.Called()
	return args.String(0)
}

func (m *ConsoleMock[T]) Args() []string {
	args := m.Called()
	return args.Get(0).([]string) //nolint
}

func (m *ConsoleMock[T]) UserConfig() *types.Config {
	args := m.Called()
	cfg := args.Get(0)

	if cfg == nil {
		return nil
	}

	return cfg.(*types.Config) //nolint
}

func (m *ConsoleMock[T]) ProfileConfig(a string) *types.Config {
	args := m.Called(a)
	cfg := args.Get(0)

	if cfg == nil {
		return nil
	}

	return cfg.(*types.Config) //nolint
}

func (m *ConsoleMock[T]) Command(name string) bool {
	args := m.Called(name)
	return args.Bool(0)
}

func (m *ConsoleMock[T]) RunSubCommand() error {
	args := m.Called()
	return args.Error(0)
}

func (m *ConsoleMock[T]) Usage(format string) {
	m.Called(format)
}

func (m *ConsoleMock[T]) Exit(code int) {
	m.Called(code)
}

func (m *ConsoleMock[T]) Printf(format string, a ...any) (int, error) {
	args := m.Called(format, a)
	return args.Int(0), args.Error(1)
}

func (m *ConsoleMock[T]) Handle(a App) (Action[T], []Option[T], error) {
	args := m.Called(a)
	return args.Get(0).(Action[T]), args.Get(1).([]Option[T]), args.Error(2) //nolint
}

func (m *ConsoleMock[T]) Run(opts ...Option[T]) error {
	args := m.Called(opts)
	return args.Error(0)
}
