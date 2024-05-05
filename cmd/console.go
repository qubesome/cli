package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	clipCmd "github.com/qubesome/cli/cmd/clipboard"
	depsCmd "github.com/qubesome/cli/cmd/deps"
	imagesCmd "github.com/qubesome/cli/cmd/images"
	runCmd "github.com/qubesome/cli/cmd/run"
	startCmd "github.com/qubesome/cli/cmd/start"
	xdgCmd "github.com/qubesome/cli/cmd/xdg"
	clip "github.com/qubesome/cli/internal/clipboard"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/profiles"
	"github.com/qubesome/cli/internal/qubesome"
	"github.com/qubesome/cli/internal/types"
)

func newConsole() *Console {
	return &Console{
		commands: map[string]any{
			"run":       runCmd.New(),
			"xdg":       xdgCmd.New(),
			"images":    imagesCmd.New(),
			"start":     startCmd.New(),
			"clipboard": clipCmd.New(),
			"deps":      depsCmd.New(),
		},
		args: os.Args,
	}
}

type Console struct {
	commands map[string]any
	args     []string
}

func (*Console) ExecName() string {
	return os.Args[0]
}

func (c *Console) Args() []string {
	return c.args
}

func (c *Console) UserConfig() *types.Config {
	path := files.QubesomeConfig()
	return config(path)
}

func (c *Console) ProfileConfig(profile string) *types.Config {
	slog.Debug("loading profile config", "profile", profile)

	path := files.ProfileConfig(profile)
	target, err := os.Readlink(path)
	if err != nil {
		slog.Debug("not able to load profile config", "path", path, "error", err)
		return nil
	}
	return config(target)
}

func (c *Console) Command(name string) bool {
	_, ok := c.commands[name]
	if !ok {
		return false
	}

	return ok
}

func (c *Console) RunSubCommand() error {
	if len(c.args) < 1 {
		return fmt.Errorf("not enough args for subcommand")
	}

	c.args = c.args[1:]
	subcmd := c.args[0]

	f, ok := c.commands[subcmd]
	if !ok {
		return fmt.Errorf("subcommand %q not found", subcmd)
	}

	if len(c.args) > 1 {
		c.args = c.args[1:]
	}

	switch f.(type) {
	case command.Handler[any]:
		return run[command.Handler[any]](f, c)
	case command.Handler[clip.Options]:
		return run[command.Handler[clip.Options]](f, c)
	case command.Handler[images.Options]:
		return run[command.Handler[images.Options]](f, c)
	case command.Handler[qubesome.Options]:
		return run[command.Handler[qubesome.Options]](f, c)
	case command.Handler[profiles.Options]:
		return run[command.Handler[profiles.Options]](f, c)
	default:
		return fmt.Errorf("subcommand type for %q is not supported", subcmd)
	}
}

func (c *Console) Usage(format string) {
	c.Printf(format, c.ExecName())
	c.Exit(1)
}

func (*Console) Exit(code int) {
	os.Exit(code)
}

func (*Console) Printf(format string, a ...any) (n int, err error) {
	return fmt.Printf(format, a...)
}

func run[K command.Handler[T], T any](handler interface{}, app command.App) error {
	action, opts, err := handler.(K).Handle(app) //nolint
	if err != nil {
		return err
	}

	return action.Run(opts...)
}

func config(path string) *types.Config {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	cfg, err := types.LoadConfig(path)
	if err != nil {
		return nil
	}
	cfg.RootDir = filepath.Dir(path)

	return cfg
}
