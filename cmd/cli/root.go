package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/log"
	"github.com/qubesome/cli/internal/types"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

var (
	targetProfile string
	sourceProfile string
	gitURL        string
	workload      string
	path          string
	local         string
	runner        string
	commandName   string
	debug         bool
	limited       bool
	interactive   bool
)

func RootCommand() *cli.Command {
	cmd := &cli.Command{
		Commands: []*cli.Command{
			startCommand(),
			runCommand(),
			imagesCommand(),
			clipboardCommand(),
			xdgCommand(),
			depsCommand(),
			doctorCommand(),
			versionCommand(),
			completionCommand(),
			hostRunCommand(),
			profileDisplayCommand(),
			flatpakCommand(),
			headlessCommand(),
			usbCommand(),
			gpuCommand(),
			superviseCommand(),
			sessionHoldCommand(),
			gatewayCommand(),
			tunnelCommand(),
			vmInitCommand(),
			consoleCommand(),
		},
	}

	// This runs ahead of every command, and several of them have a caller
	// reading their standard output as data rather than as text for a
	// person: tunnel puts an ssh connection through it, completion is
	// eval'd by a shell, and clipboard writes back what was pasted. A
	// warning on stdout is read as the far end talking, as shell input, or
	// as part of the paste, so it goes to stderr for the same reason
	// main.go sends a returned error there. It is still seen: stderr is
	// where ssh shows what a ProxyCommand says.
	cmd.Before = func(ctx context.Context, c *cli.Command) (context.Context, error) {
		// The level is decided here rather than in the flag's own Action
		// so that there is one place that sets it and no question about
		// which of the two runs last.
		//
		// A run says what it did at INFO whether or not --debug was
		// asked for: which session it is in, which gateway it used, what
		// address it was given, what it wired. Those are the facts a
		// launch that goes wrong is diagnosed from, and leaving them
		// behind --debug meant the first report of any failure never had
		// them.
		level := "INFO"
		if debug {
			level = "DEBUG"
		}

		if err := log.Configure(level, true, false, false); err != nil {
			return ctx, err
		}

		if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") {
			fmt.Fprintln(os.Stderr,
				"\033[33mWARN: Running qubesome in Wayland is experimental. Some features may not work as expected.\033[0m")
		}
		return ctx, nil
	}

	cmd.Flags = append(cmd.Flags, &cli.BoolFlag{
		Name: "debug",
		// Every other flag says what it is for, and this one printed a
		// bare name in the global options of every help screen.
		Usage:       "log at DEBUG rather than INFO",
		Value:       false,
		Destination: &debug,
		Sources:     cli.EnvVars("QS_DEBUG"),
	})
	cmd.Version = shortVersion()
	cmd.Usage = "A cli to GitOps your dotfiles"
	cmd.Suggest = true
	cmd.EnableShellCompletion = true

	return cmd
}

func config(path string) *types.Config {
	if _, err := os.Stat(path); err != nil {
		return nil
	}

	// A started profile's config is reached through a symlink in the run
	// dir. The root dir has to be the directory the config was sourced
	// from, since a profile's path and every path mapped into it descend
	// from that, and the run dir holds none of them.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	cfg, err := types.LoadConfig(path)
	if err != nil {
		return nil
	}
	cfg.Source = path
	cfg.RootDir = filepath.Dir(path)

	// Remembered here because this is the one place every config that is
	// successfully opened passes through, whichever flag or profile or
	// fallback named it.
	files.RememberConfig(path)

	return cfg
}

// rememberedConfig offers the last config qubesome opened, and returns it
// only if the user says to use it.
//
// A machine usually has one config, and it usually lives in a dotfiles
// repository whose path nobody wants to type. Once every profile has
// stopped there is nothing running to find it through, so a bare
// qubesome run had no config and could only say so.
//
// It asks rather than assuming. Falling back silently would run a
// workload against a config the user has not thought about since the last
// time they used it, possibly from a repository they have since moved on
// from, and the whole point of the record is that it outlives the session
// that wrote it.
func rememberedConfig() *types.Config {
	rememberedOnce.Do(func() {
		path, ok := files.RememberedConfig()
		if !ok {
			return
		}

		if !confirm("no profile is running and no config was given. Use the last config qubesome opened?\n  " + path) {
			return
		}

		remembered = config(path)
	})

	return remembered
}

// remembered holds the answer for the length of the process. Resolving a
// profile reaches for the config and then reaches for it again to find
// the profile in it, and a question asked twice for one command is a
// question the user starts answering without reading.
var (
	rememberedOnce sync.Once
	remembered     *types.Config
)

// confirm asks a yes or no question on the terminal, defaulting to yes.
//
// It answers no whenever there is no terminal to ask on, and that is the
// case that decides the shape of this. A workload is usually launched
// from a window manager keybinding or over the profile's RPC, where
// standard input is not a terminal and nobody would ever see the
// question, and a prompt there would be a launch that hangs for good. The
// caller reports what it would have reported instead, and names the path
// so the answer is still in front of the user.
func confirm(question string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}

	// Asked on stderr, for the reason the logging goes there: stdout of a
	// qubesome command is read by ssh, by a shell and by a paste.
	fmt.Fprintf(os.Stderr, "%s [Y/n] ", question)

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

func profileConfigOrDefault(profile string) *types.Config {
	if profile != "" {
		// Try to load the profile specific config.
		path := files.ProfileConfig(profile)
		target, err := os.Readlink(path)
		slog.Debug("try to load profile config", "profile", profile, "path", path, target, "target")

		if err == nil {
			c := config(target)
			slog.Debug("using profile config", "path", path, "config", c)
			if c != nil {
				return c
			}
		}
	}

	cfgs := activeConfigs()
	if len(cfgs) == 1 {
		c := config(cfgs[0])
		slog.Debug("using active profile config", "path", cfgs[0], "config", c)
		if c != nil && len(c.Profiles) > 0 {
			return c
		}
	}

	// Try to load user-level qubesome config.
	path = files.QubesomeConfig()
	c := config(path)
	slog.Debug("using user-level config", "path", path, "config", c)
	if c != nil && len(c.Profiles) > 0 {
		return c
	}

	// Last of all, the config qubesome opened the last time. Nothing is
	// running, there is no user-level config, and this is the only thing
	// left that knows where a config lives. It is the end of the chain
	// rather than anywhere earlier because it is the only step that asks
	// the user a question.
	if c := rememberedConfig(); c != nil && len(c.Profiles) > 0 {
		return c
	}

	return nil
}

func profileOrActive(profile string) (*types.Profile, error) {
	if profile != "" {
		cfg := profileConfigOrDefault(profile)
		prof, ok := cfg.Profile(profile)
		if !ok {
			return nil, fmt.Errorf("profile %q not active", profile)
		}
		return prof, nil
	}

	cfgs := activeConfigs()
	if len(cfgs) > 1 {
		return nil, errors.New("multiple profiles active: pick one with -profile")
	}

	if len(cfgs) == 1 {
		f := cfgs[0]
		name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))

		return profileOrActive(name)
	}

	// Nothing is running, so there is no active profile to pick and the
	// question becomes which config to read one out of.
	return soleProfile(profileConfigOrDefault(""))
}

// soleProfile returns the one profile of cfg.
//
// It is only reached with nothing running, where the config has come from
// the user-level file or from the one qubesome last opened. One profile
// in it is an answer; more than one is the ambiguity -profile has always
// existed to settle, and it is reported as such rather than guessed at.
func soleProfile(cfg *types.Config) (*types.Profile, error) {
	if cfg == nil || len(cfg.Profiles) == 0 {
		if path, ok := files.RememberedConfig(); ok {
			// There is a record, and it was either declined or there was
			// no terminal to offer it on. The path is the useful half of
			// the message either way.
			return nil, fmt.Errorf("no active profile found: start one with qubesome start, "+
				"or name a profile of the last config qubesome opened (%s) with -profile", path)
		}

		return nil, errors.New("no active profile found: start one with qubesome start")
	}

	if len(cfg.Profiles) > 1 {
		return nil, fmt.Errorf("no profile is running and %s has %d profiles: pick one with -profile",
			cfg.Source, len(cfg.Profiles))
	}

	// By the key rather than by the value's own Name, which a config is
	// not obliged to repeat inside each profile.
	for name := range cfg.Profiles {
		prof, ok := cfg.Profile(name)
		if !ok {
			break
		}
		prof.Name = name

		return prof, nil
	}

	return nil, errors.New("no active profile found: start one with qubesome start")
}

func activeConfigs() []string {
	var active []string

	root := files.RunUserQubesome()
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			fn := entry.Name()
			if filepath.Ext(fn) == ".config" {
				active = append(active, filepath.Join(root, fn))
			}
		}
	}

	return active
}

func activeProfiles() []string {
	cfgs := activeConfigs()
	profiles := make([]string, 0, len(cfgs))

	for _, file := range cfgs {
		profiles = append(profiles, strings.TrimSuffix(filepath.Base(file), ".config"))
	}

	return profiles
}
