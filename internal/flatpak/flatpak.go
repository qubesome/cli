package flatpak

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/inception"
)

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if err := o.Validate(); err != nil {
		return err
	}

	if inception.Inside() {
		client := inception.NewClient(files.InProfileSocketPath())
		return client.FlatpakRun(context.TODO(), o.Name, o.ExtraArgs)
	}

	if o.Config == nil {
		return fmt.Errorf("no config found")
	}

	prof, ok := o.Config.Profiles[o.Profile]
	if !ok {
		return fmt.Errorf("cannot find profile %q", o.Profile)
	}

	allowed := false
	for _, name := range prof.Flatpaks {
		if name == o.Name {
			allowed = true
			break
		}
	}

	if !allowed {
		return fmt.Errorf("flatpak %q is not allowed for profile %q", o.Name, o.Profile)
	}

	args := []string{"run", o.Name}
	args = append(args, o.ExtraArgs...)

	c := exec.Command("/usr/bin/flatpak", args...)
	c.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=:%d", prof.Display))
	out, err := c.CombinedOutput()
	fmt.Println(string(out))

	return err
}
