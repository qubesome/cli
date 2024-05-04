package command

import (
	"github.com/qubesome/cli/internal/types"
)

type Option[T any] func(*T)

type Action[T any] interface {
	Run(opts ...Option[T]) error
}

type Handler[T any] interface {
	Handle(App) (Action[T], []Option[T], error)
}

type App interface {
	ExecName() string
	Args() []string

	UserConfig() *types.Config
	ProfileConfig(profile string) *types.Config

	Command(name string) bool
	RunSubCommand() error

	Usage(format string)
	Exit(code int)
	Printf(format string, a ...interface{}) (n int, err error)
}
