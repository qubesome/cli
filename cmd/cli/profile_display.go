package cli

import (
	"context"
	"fmt"

	"github.com/qubesome/cli/internal/profiles"
	"github.com/urfave/cli/v3"
)

// profileDisplayCommand is the entrypoint of the profile container. It is
// hidden because it is not useful on the host: it starts a compositor and
// an X server that only exist inside a profile.
func profileDisplayCommand() *cli.Command {
	var (
		display       uint
		geometry      string
		authFile      string
		windowManager string
		extraArgs     string
		fullscreen    bool
	)

	return &cli.Command{
		Name:   "profile-display",
		Usage:  "Runs the display stack inside a qubesome profile",
		Hidden: true,
		Flags: []cli.Flag{
			&cli.UintFlag{Name: "display", Destination: &display, Required: true},
			&cli.StringFlag{Name: "geometry", Destination: &geometry, Required: true},
			&cli.StringFlag{Name: "auth", Destination: &authFile, Required: true},
			&cli.StringFlag{Name: "wm", Destination: &windowManager, Required: true},
			&cli.StringFlag{Name: "extra", Destination: &extraArgs},
			&cli.BoolFlag{Name: "fullscreen", Destination: &fullscreen},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if display > 255 {
				return fmt.Errorf("display %d is out of range", display)
			}

			return profiles.RunDisplayWithOptions(profiles.DisplayOptions{
				Display:       uint8(display),
				Geometry:      geometry,
				AuthFile:      authFile,
				WindowManager: windowManager,
				ExtraArgs:     extraArgs,
				Fullscreen:    fullscreen,
			})
		},
	}
}
