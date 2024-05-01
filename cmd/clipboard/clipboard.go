package clipboard

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/qubesome/cli/internal/clipboard"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
)

func Command(args []string, cfg *types.Config) error {
	var t string
	var fromHost bool
	var fromProfile string

	f := flag.NewFlagSet("", flag.ExitOnError)
	f.StringVar(&t, "type", "", "The target type for xclip.")
	f.StringVar(&fromProfile, "from-profile", "", "The profile to copy the clipboard from. Cannot be used with --from-host.")
	f.BoolVar(&fromHost, "from-host", false, "Use the host clipboard as source. Cannot be used with --from-profile.")
	err := f.Parse(args)
	if err != nil {
		return fmt.Errorf("failed to parse args: %w", err)
	}

	slog.Debug("cmd", "args", args)

	if len(f.Args()) != 1 {
		clipboardUsage(os.Args[0])
	}

	toProfile := f.Arg(0)
	// If user level config was not found, try to load the config
	// for the target profile which at this point must be started.
	if cfg == nil {
		cfgPath := files.ProfileConfig(toProfile)
		slog.Debug("trying to load config from started profile", "path", cfgPath)

		cfg, err = types.LoadConfig(cfgPath)
		if err != nil {
			return fmt.Errorf("could load config (check profile is loaded): %w", err)
		}
	}

	from := uint8(0)
	if fromHost && len(fromProfile) > 0 {
		return fmt.Errorf("err: --from-host cannot be used with --from-profile")
	}
	if len(fromProfile) > 0 {
		p, ok := cfg.Profiles[fromProfile]
		if !ok {
			return fmt.Errorf("from profile %s not found", fromProfile)
		}

		from = p.Display
	}

	var to *types.Profile
	p, ok := cfg.Profiles[toProfile]
	if !ok {
		return fmt.Errorf("profile %s not found", toProfile)
	}
	to = p

	slog.Debug("clipboard copy", "from", from, "to", to, "type", t)
	return clipboard.Copy(from, to, t)
}

func clipboardUsage(name string) {
	fmt.Printf(
		"usage: %[1]s clipboard --from-profile <profile_name> <profile_to>\n"+
			"       %[1]s clipboard --type image/png --from-host <profile_to>\n"+
			"       %[1]s clipboard --from-host <profile_to>\n",
		name)
	os.Exit(1)
}