package clipboard

import (
	"flag"
	"fmt"

	"github.com/qubesome/cli/internal/clipboard"
	"github.com/qubesome/cli/internal/command"
)

const usage = `usage:
    %[1]s clipboard --from-profile <profile_name> <profile_to>
    %[1]s clipboard --type image/png --from-host <profile_to>
    %[1]s clipboard --from-host <profile_to>
`

type handler struct {
}

func New() command.Handler[clipboard.Options] {
	return &handler{}
}

func (c *handler) Handle(in command.App) (command.Action[clipboard.Options], []command.Option[clipboard.Options], error) {
	var contentType string
	var fromHost bool
	var fromProfile string

	f := flag.NewFlagSet("", flag.ContinueOnError)
	f.StringVar(&contentType, "type", "", "The content type for xclip.")
	f.StringVar(&fromProfile, "from-profile", "", "The profile to copy the clipboard from. Cannot be used with --from-host.")
	f.BoolVar(&fromHost, "from-host", false, "Use the host clipboard as source. Cannot be used with --from-profile.")
	err := f.Parse(in.Args())
	if err != nil {
		return nil, nil, err
	}

	if len(f.Args()) != 1 ||
		(!fromHost && fromProfile == "") || // need at least one
		(fromHost && fromProfile != "") { // can't have both
		in.Usage(usage)
		return nil, nil, nil
	}

	var opts []command.Option[clipboard.Options]

	if fromHost {
		opts = append(opts, clipboard.WithFromHost())
	}

	toProfile := f.Arg(0)
	cfg := in.UserConfig()
	if cfg == nil {
		cfg = in.ProfileConfig(toProfile)
	}

	if cfg == nil {
		return nil, nil, fmt.Errorf("no config found")
	}

	if fromProfile != "" {
		p, ok := cfg.Profiles[fromProfile]
		if !ok {
			return nil, nil, fmt.Errorf("source profile %s not found", fromProfile)
		}
		opts = append(opts, clipboard.WithSourceProfile(p))
	}

	p, ok := cfg.Profiles[toProfile]
	if !ok {
		return nil, nil, fmt.Errorf("target profile %s not found", toProfile)
	}
	opts = append(opts, clipboard.WithTargetProfile(p))

	if contentType != "" {
		opts = append(opts, clipboard.WithContentType(contentType))
	}

	return c, opts, nil
}

func (c *handler) Run(opts ...command.Option[clipboard.Options]) error {
	return clipboard.Run(opts...)
}
