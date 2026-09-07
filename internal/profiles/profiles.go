package profiles

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/client"
	"github.com/go-git/go-git/v6/plumbing/transport/ssh"
	"github.com/google/uuid"
	"github.com/qubesome/cli/internal/command"
	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/keyring"
	"github.com/qubesome/cli/internal/keyring/backend"
	"github.com/qubesome/cli/internal/runners/util/container"
	"github.com/qubesome/cli/internal/sandbox"
	"github.com/qubesome/cli/internal/seccomp"
	"github.com/qubesome/cli/internal/types"
	"github.com/qubesome/cli/internal/util/dbus"
	"github.com/qubesome/cli/internal/util/drive"
	"github.com/qubesome/cli/internal/util/env"
	"github.com/qubesome/cli/internal/util/gpu"
	"github.com/qubesome/cli/internal/util/mtls"
	"github.com/qubesome/cli/internal/util/resolution"
	"github.com/qubesome/cli/internal/util/xauth"
	"github.com/qubesome/cli/pkg/inception"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/execabs"
	"golang.org/x/term"
)

// The user a profile image runs as. The X cookies and the desktop files
// are mounted into this home, so the profile image has to provide it, and
// the sandbox environment names the same user rather than trusting
// whatever the image left in its own environment.
const (
	profileUser = "xorg-user"
	profileHome = "/home/" + profileUser
)

var (
	ContainerNameFormat = "qubesome-%s"
	defaultProfileImage = "ghcr.io/qubesome/xorg:latest"

	appTemplate = `[Desktop Entry]
Version=1.0
Name={{.Name}}
Exec=/bin/sh -c "/usr/local/bin/qubesome run {{.Name}} %U"
Icon=qubesome-generic
StartupNotify=true
Terminal=false
Type=Application
`
	qubesomeIconBlack = `<svg xmlns="http://www.w3.org/2000/svg" version="1.1" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:svgjs="http://svgjs.dev/svgjs" width="2000" height="1250" viewBox="0 0 2000 1250"><g transform="matrix(1,0,0,1,0,0)"><svg viewBox="0 0 512 320" data-background-color="#ffffff" preserveAspectRatio="xMidYMid meet" height="1250" width="2000" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><g id="tight-bounds" transform="matrix(1,0,0,1,0,0)"><svg viewBox="0 0 512 320" height="320" width="512"><g><svg></svg></g><g><svg viewBox="0 0 512 320" height="320" width="512"><g><path transform="translate(256,160) scale(226.27417,226.27417)" d="M-0.707 0.707l0-1.414 1.414 0 0 1.414z" fill="#000000" fill-rule="nonzero" stroke="none" stroke-width="1" stroke-linecap="butt" stroke-linejoin="miter" stroke-miterlimit="10" stroke-dasharray="" stroke-dashoffset="0" font-family="none" font-weight="none" font-size="none" text-anchor="none" style="mix-blend-mode: normal" data-fill-palette-color="tertiary"></path></g><g transform="matrix(1,0,0,1,140.8,131.0465864758607)"><svg viewBox="0 0 230.4 57.90682704827861" height="57.90682704827861" width="230.4"><g><svg viewBox="0 0 313.6537803795311 78.83114241960186" height="57.90682704827861" width="230.4"><g transform="matrix(1,0,0,1,83.25378037953108,10.894897396630942)"><svg viewBox="0 0 230.40000000000003 57.04134762633998" height="57.04134762633998" width="230.40000000000003"><g id="textblocktransform"><svg viewBox="0 0 230.40000000000003 57.04134762633998" height="57.04134762633998" width="230.40000000000003" id="textblock"><g><svg viewBox="0 0 230.40000000000003 57.04134762633998" height="57.04134762633998" width="230.40000000000003"><g transform="matrix(1,0,0,1,0,0)"><svg width="230.40000000000003" viewBox="1.7 -37.65 195.88000000000002 48.5" height="57.04134762633998" data-palette-color="#ffffff"><path d="M16.6-26.45L21.95-26.45 21.95 10.85 15.8 10.15 15.8-2.65Q13.7 0.75 10.1 0.75L10.1 0.75Q6.1 0.75 3.9-2.9 1.7-6.55 1.7-13.2L1.7-13.2Q1.7-17.45 2.83-20.6 3.95-23.75 5.95-25.45 7.95-27.15 10.6-27.15L10.6-27.15Q14.05-27.15 16.3-23.95L16.3-23.95 16.6-26.45ZM11.85-3.9Q13.05-3.9 14-4.7 14.95-5.5 15.8-7.1L15.8-7.1 15.8-19.8Q14.1-22.55 12.1-22.55L12.1-22.55Q10.3-22.55 9.2-20.33 8.1-18.1 8.1-13.25L8.1-13.25Q8.1-8.1 9.08-6 10.05-3.9 11.85-3.9L11.85-3.9ZM45.8-26.45L45.8 0 40.45 0 40.15-3.35Q38.9-1.3 37.35-0.28 35.8 0.75 33.75 0.75L33.75 0.75Q30.85 0.75 29.17-1.3 27.5-3.35 27.5-6.65L27.5-6.65 27.5-26.45 33.65-26.45 33.65-7Q33.65-3.9 35.75-3.9L35.75-3.9Q36.95-3.9 37.9-4.83 38.85-5.75 39.65-7.45L39.65-7.45 39.65-26.45 45.8-26.45ZM63.65-27.15Q67.5-27.15 69.55-23.65 71.59-20.15 71.59-13.25L71.59-13.25Q71.59-6.75 69.3-3 67 0.75 63 0.75L63 0.75Q61.25 0.75 59.8-0.13 58.35-1 57.3-2.55L57.3-2.55 56.95 0 51.5 0 51.5-37 57.65-37.65 57.65-23.6Q58.7-25.3 60.22-26.23 61.75-27.15 63.65-27.15L63.65-27.15ZM61.35-3.9Q63.2-3.9 64.22-6 65.25-8.1 65.25-13.25L65.25-13.25Q65.25-18.6 64.32-20.58 63.4-22.55 61.7-22.55L61.7-22.55Q60.55-22.55 59.5-21.6 58.45-20.65 57.65-19.1L57.65-19.1 57.65-6.7Q58.3-5.45 59.27-4.67 60.25-3.9 61.35-3.9L61.35-3.9ZM95.04-13.85Q95.04-13.4 94.89-11.3L94.89-11.3 81.24-11.3Q81.44-7.2 82.67-5.58 83.89-3.95 86.14-3.95L86.14-3.95Q87.69-3.95 88.94-4.48 90.19-5 91.64-6.15L91.64-6.15 94.19-2.65Q90.69 0.75 85.79 0.75L85.79 0.75Q80.59 0.75 77.82-2.85 75.04-6.45 75.04-13L75.04-13Q75.04-19.55 77.69-23.35 80.34-27.15 85.19-27.15L85.19-27.15Q89.84-27.15 92.44-23.78 95.04-20.4 95.04-13.85L95.04-13.85ZM89.04-15.3L89.04-15.65Q89.04-19.4 88.14-21.13 87.24-22.85 85.24-22.85L85.24-22.85Q83.39-22.85 82.42-21.18 81.44-19.5 81.24-15.3L81.24-15.3 89.04-15.3ZM106.69-27.15Q111.39-27.15 114.64-24.05L114.64-24.05 112.29-20.7Q110.89-21.7 109.64-22.2 108.39-22.7 107.09-22.7L107.09-22.7Q105.64-22.7 104.82-21.93 103.99-21.15 103.99-19.8L103.99-19.8Q103.99-18.45 104.92-17.63 105.84-16.8 108.64-15.7L108.64-15.7Q112.09-14.35 113.74-12.5 115.39-10.65 115.39-7.55L115.39-7.55Q115.39-3.7 112.69-1.48 109.99 0.75 105.84 0.75L105.84 0.75Q103.14 0.75 100.87-0.23 98.59-1.2 96.89-2.95L96.89-2.95 99.89-6.25Q102.69-3.8 105.59-3.8L105.59-3.8Q107.24-3.8 108.19-4.67 109.14-5.55 109.14-7.1L109.14-7.1Q109.14-8.25 108.74-8.97 108.34-9.7 107.34-10.33 106.34-10.95 104.34-11.7L104.34-11.7Q100.89-13.05 99.42-14.85 97.94-16.65 97.94-19.45L97.94-19.45Q97.94-22.8 100.32-24.98 102.69-27.15 106.69-27.15L106.69-27.15ZM128.64-27.15Q133.64-27.15 136.41-23.65 139.19-20.15 139.19-13.25L139.19-13.25Q139.19-6.65 136.39-2.95 133.59 0.75 128.64 0.75L128.64 0.75Q123.69 0.75 120.89-2.88 118.09-6.5 118.09-13.25L118.09-13.25Q118.09-19.95 120.89-23.55 123.69-27.15 128.64-27.15L128.64-27.15ZM128.64-22.5Q126.49-22.5 125.49-20.38 124.49-18.25 124.49-13.25L124.49-13.25Q124.49-8.2 125.49-6.08 126.49-3.95 128.64-3.95L128.64-3.95Q130.79-3.95 131.79-6.08 132.79-8.2 132.79-13.25L132.79-13.25Q132.79-18.3 131.79-20.4 130.79-22.5 128.64-22.5L128.64-22.5ZM167.39-27.15Q170.04-27.15 171.64-25.1 173.24-23.05 173.24-19.6L173.24-19.6 173.24 0 167.19 0 167.19-18.95Q167.19-20.85 166.69-21.68 166.19-22.5 165.24-22.5L165.24-22.5Q163.29-22.5 161.49-18.8L161.49-18.8 161.49 0 155.49 0 155.49-18.95Q155.49-22.5 153.59-22.5L153.59-22.5Q151.54-22.5 149.79-18.8L149.79-18.8 149.79 0 143.74 0 143.74-26.45 149.09-26.45 149.49-23.05Q152.09-27.15 155.74-27.15L155.74-27.15Q157.59-27.15 158.94-26 160.29-24.85 160.99-22.75L160.99-22.75Q163.64-27.15 167.39-27.15L167.39-27.15ZM197.58-13.85Q197.58-13.4 197.43-11.3L197.43-11.3 183.78-11.3Q183.98-7.2 185.21-5.58 186.43-3.95 188.68-3.95L188.68-3.95Q190.23-3.95 191.48-4.48 192.73-5 194.18-6.15L194.18-6.15 196.73-2.65Q193.23 0.75 188.33 0.75L188.33 0.75Q183.13 0.75 180.36-2.85 177.58-6.45 177.58-13L177.58-13Q177.58-19.55 180.23-23.35 182.88-27.15 187.73-27.15L187.73-27.15Q192.38-27.15 194.98-23.78 197.58-20.4 197.58-13.85L197.58-13.85ZM191.58-15.3L191.58-15.65Q191.58-19.4 190.68-21.13 189.78-22.85 187.78-22.85L187.78-22.85Q185.93-22.85 184.96-21.18 183.98-19.5 183.78-15.3L183.78-15.3 191.58-15.3Z" opacity="1" transform="matrix(1,0,0,1,0,0)" fill="#ffffff" class="wordmark-text-0" data-fill-palette-color="quaternary" id="text-0"></path></svg></g></svg></g></svg></g></svg></g><g><svg viewBox="0 0 69.78768719729524 78.83114241960186" height="78.83114241960186" width="69.78768719729524"><g><svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" xml:space="preserve" version="1.1" style="shape-rendering:geometricPrecision;text-rendering:geometricPrecision;image-rendering:optimizeQuality;" viewBox="19 15.25 61 68.90470053837926" x="0" y="0" fill-rule="evenodd" clip-rule="evenodd" height="78.83114241960186" width="69.78768719729524" class="icon-icon-0" data-fill-palette-color="quaternary" id="icon-0"><g fill="#ffffff" data-fill-palette-color="quaternary"><path class="" d="M53 16C62 21 69 25 77 30 79 31 80 32 80 35V64C80 66 79 68 77 69 68 74 62 78 53 83 50 84 49 85 46 83 37 78 30 74 22 69 20 68 19 67 19 64V35C19 33 20 32 22 30 31 25 38 21 47 16 49 15 51 15 53 16M52 51V78C60 73 68 69 75 64V37zM48 78V51L25 37V64C33 69 41 73 48 78M27 34L50 48 73 34C65 29 57 25 50 20 42 25 34 29 27 34" fill="#ffffff" fill-rule="nonzero" data-fill-palette-color="quaternary"></path></g></svg></g></svg></g></svg></g></svg></g></svg></g><defs></defs></svg><rect width="512" height="320" fill="none" stroke="none" visibility="hidden"></rect></g></svg></g></svg>`
)

func Run(opts ...command.Option[Options]) error {
	o := &Options{}
	for _, opt := range opts {
		opt(o)
	}

	if o.GitURL != "" {
		return StartFromGit(o.Runner, o.Profile, o.GitURL, o.Path, o.Local, o.Interactive)
	}

	if o.Local != "" {
		// Running from local, treat the local path as the GITDIR for envvar
		// expansion purposes.
		err := env.Update("GITDIR", o.Local)
		if err != nil {
			return err
		}
	}

	path := filepath.Join(o.Local, o.Path, "qubesome.config")
	if _, err := os.Stat(path); err != nil {
		return err
	}
	cfg, err := types.LoadConfig(path)
	if err != nil {
		return err
	}

	if cfg == nil {
		return fmt.Errorf("cannot start profile: nil config")
	}

	cfg.RootDir = filepath.Dir(path)
	profile, ok := cfg.Profile(o.Profile)
	if !ok {
		return fmt.Errorf("cannot start profile: profile %q not found", o.Profile)
	}

	return Start(o.Runner, profile, cfg, o.Interactive)
}

func validGitDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}

	if !fi.IsDir() {
		return false
	}

	// Confirm the repository exists and is a valid Git repository.
	_, err = git.PlainOpen(path)
	return err == nil
}

// sandboxStatePath returns where a running profile sandbox is recorded.
func sandboxStatePath(profile string) string {
	return filepath.Join(files.ProfileDir(profile), "sandbox.json")
}

// errAlreadyStarted reports a profile whose sandbox is still running.
func errAlreadyStarted(profile string) error {
	return fmt.Errorf("profile %q is already started", profile)
}

func StartFromGit(runner, name, gitURL, path, local string, interactive bool) error {
	ln := files.ProfileConfig(name)

	if _, err := os.Lstat(ln); err == nil {
		// Start checks this too, and has to, because it is also reached
		// without a symlink. The check stays here as well so a running
		// profile does not lose its config symlink on the way to that
		// error.
		if sandbox.Alive(sandboxStatePath(name)) {
			return errAlreadyStarted(name)
		}

		if err = os.Remove(ln); err != nil {
			return fmt.Errorf("failed to remove leftover profile symlink: %w", err)
		}
	}

	dir, err := files.GitDirPath(gitURL)
	if err != nil {
		return err
	}

	if strings.HasPrefix(local, "~") {
		if len(local) > 1 && local[1] == '/' {
			local = filepath.Join(os.ExpandEnv("${HOME}"), local[1:])
		}
	}

	if local != "" && validGitDir(local) { //nolint
		slog.Debug("start from local", "path", local)
		dir = local
	} else if validGitDir(dir) {
		slog.Debug("start from existing cloned repo", "path", dir)
	} else {
		slog.Debug("cloning repo to start")

		var clientOptions []client.Option
		if strings.HasPrefix(gitURL, "git@") {
			a, err := ssh.NewSSHAgentAuth("git")
			if err != nil {
				return err
			}
			clientOptions = append(clientOptions, client.WithSSHAuth(a))
		}

		_, err = git.PlainClone(dir, &git.CloneOptions{
			URL:           gitURL,
			ClientOptions: clientOptions,
		})
		if err != nil {
			return err
		}
	}

	// This is a Git setup, and now we know the Git Dir, so enable
	// environment variable GITDIR expansion.
	err = env.Update("GITDIR", dir)
	if err != nil {
		return err
	}

	// Get the qubesome config from the Git repository.
	cfgPath, err := securejoin.SecureJoin(dir, filepath.Join(path, "qubesome.config"))
	if err != nil {
		return err
	}

	cfg, err := types.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(ln), files.DirMode)
	if err != nil {
		return err
	}

	err = os.Symlink(cfgPath, ln)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(ln)
	}()

	p, ok := cfg.Profile(name)
	if !ok {
		return fmt.Errorf("cannot file profile %q in config %q", name, cfgPath)
	}

	// When sourcing from git, ensure profile path is relative to the git repository.
	pp, err := securejoin.SecureJoin(filepath.Dir(cfgPath), p.Path)
	if err != nil {
		return err
	}
	p.Path = pp

	slog.Debug("start from git", "profile", p.Name, "p", path, "path", p.Path, "config", cfgPath)

	return Start(runner, p, cfg, interactive)
}

func Start(runner string, profile *types.Profile, cfg *types.Config, interactive bool) (err error) {
	if cfg == nil {
		return fmt.Errorf("cannot start profile: config is nil")
	}

	if profile.Image == "" {
		slog.Debug("no profile image set, using default instead", "default-image", defaultProfileImage)
		profile.Image = defaultProfileImage
	}

	if err := profile.Validate(); err != nil {
		return err
	}

	// Both entry paths land here, and docker used to refuse a second
	// start through the container name. bwrap has no such thing, so a
	// second start would truncate the running profile's X cookies, race
	// for its display, and on the way out delete its socket, shm backing
	// and runtime dir.
	if sandbox.Alive(sandboxStatePath(profile.Name)) {
		return errAlreadyStarted(profile.Name)
	}

	// If runner is not being overwritten (via -runner), use the runner
	// set at profile level in the config.
	if runner == "" && profile.Runner != "" {
		runner = profile.Runner
	}

	bundle, err := images.PullProfileImage(profile.Image)
	if err != nil {
		return fmt.Errorf("cannot prepare profile image: %w", err)
	}

	// Workloads still run under the container runner, so their images are
	// still its to pull.
	binary := files.ContainerRunnerBinary(runner)

	imgs, err := images.MissingImages(binary, cfg)
	if err != nil {
		return err
	}

	if len(imgs) > 0 && term.IsTerminal(int(os.Stdout.Fd())) {
		if proceed("Not all workload images are present. Start loading them on the background?") {
			go images.PreemptWorkloadImages(binary, cfg)
		}
	}

	if profile.Gpus != "" {
		switch {
		case gpu.NvidiaToolkitPresent():
			profile.Gpus = ""
			dbus.NotifyOrLog("qubesome error",
				"GPU passthrough for nvidia is not supported for profiles, disabling it")
		default:
			if _, _, err := gpu.SandboxEdits("/"); err != nil {
				profile.Gpus = ""
				dbus.NotifyOrLog("qubesome error", "GPU support was not detected, disabling it for qubesome")
			}
		}
	}

	// Block profile from starting if external drive is not available.
	if len(profile.ExternalDrives) > 0 {
		slog.Debug("profile has required external drives", "drives", profile.ExternalDrives)
		for _, dm := range profile.ExternalDrives {
			split := strings.Split(dm, ":")
			if len(split) != 3 {
				return fmt.Errorf("cannot enforce external drive: invalid format")
			}

			label := split[0]
			ok, err := drive.Mounts(split[1], split[2])
			if err != nil {
				return fmt.Errorf("cannot check drive label mounts: %w", err)
			}

			if !ok {
				dbus.NotifyOrLog("qubesome start error", fmt.Sprintf("required drive %s is not mounted at %s", split[0], split[1]))

				return fmt.Errorf("required drive %q is not mounted at %q", split[0], split[1])
			}

			env.Add(label, split[2])
		}
	}

	err = createMagicCookie(profile)
	if err != nil {
		return err
	}

	sockPath, err := files.SocketPath(profile.Name)
	if err != nil {
		return err
	}

	creds, err := mtls.NewCredentials()
	if err != nil {
		return err
	}

	// A profile that was killed rather than stopped leaves its socket
	// behind, and listening on a path that already exists fails. That
	// left the profile running with nothing serving it, and every
	// workload it launched failing to reach the host.
	//
	// Only a socket is removed, and Lstat rather than Stat so that a
	// symlink is judged as itself rather than as whatever it points at.
	// Anything else at that path is corruption or a mistake, and this
	// runs precisely when the profile directory is already in a state
	// nobody intended, so deleting whatever happened to be there would
	// be worse than refusing to start.
	fi, serr := os.Lstat(sockPath)
	switch {
	case serr != nil && !errors.Is(serr, fs.ErrNotExist):
		return fmt.Errorf("failed to stat profile socket %q: %w", sockPath, serr)

	case serr == nil && fi.Mode().Type()&os.ModeSocket == 0:
		return fmt.Errorf("%q exists and is not a socket, so it was left alone: "+
			"check what it is and remove it to start this profile", sockPath)

	case serr == nil:
		if err := os.Remove(sockPath); err != nil {
			return fmt.Errorf("failed to remove stale socket %q: %w", sockPath, err)
		}
	}

	go func() {
		server := inception.NewServer(profile, cfg)
		if err := server.Listen(creds.ServerCert, creds.CA, sockPath); err != nil {
			// Reported rather than recorded, because the profile is
			// already starting and this is the only sign that nothing
			// will answer it.
			slog.Error("profile socket is not being served", "error", err)
			dbus.NotifyOrLog("qubesome start error",
				fmt.Sprintf("profile %s cannot serve workloads: %v", profile.Name, err))
		}
	}()

	defer func() {
		// Clean up the profile dir once profile finishes. The error is
		// kept out of the named return, which already carries the reason
		// the start ended and must not be replaced by a successful
		// removal.
		pd := files.ProfileDir(profile.Name)
		if rerr := os.RemoveAll(pd); rerr != nil {
			slog.Warn("failed to remove profile dir", "path", pd, "error", rerr)
		}
	}()

	defer func() {
		err := deleteMtlsData(profile.Name)
		if err != nil {
			slog.Warn("failed to delete mTLS data", "error", err)
		}
	}()

	cmd, err := createNewDisplay(bundle, creds.CA, creds.ClientPEM, creds.ClientKeyPEM,
		profile, strconv.Itoa(int(profile.Display)), interactive, cfg)
	if err != nil {
		slog.Warn("failed to create display", "error", err)
		return err
	}

	if err := sandbox.WriteState(sandboxStatePath(profile.Name), cmd.Process.Pid); err != nil {
		// The state file is the gate that stops a second start from
		// trampling this one, so a sandbox that cannot be recorded must
		// not keep running. Recording also fails when the sandbox is
		// already gone, and reporting that beats waiting forever on a
		// profile that is not there.
		if kerr := cmd.Process.Kill(); kerr != nil {
			slog.Warn("failed to kill the unrecorded sandbox", "error", kerr)
		}
		_ = cmd.Wait()

		return fmt.Errorf("failed to record sandbox state: %w", err)
	}

	// The host process serves this profile's socket for as long as the
	// profile runs, so it waits here rather than returning. The sandbox is
	// no longer detached, so its exit is observed directly rather than by
	// polling the container runner, and --die-with-parent closes the same
	// loop from the other direction: a qubesome that dies no longer leaves
	// a profile running with nothing attached to it.
	if werr := awaitSandbox(profile.Name, cmd); werr != nil {
		// Notified as well as returned. A profile is usually started from
		// a key binding or an autostart entry rather than from a
		// terminal, and there nothing shows the returned error. Whoever
		// does have a terminal already has the reason on it, because the
		// sandbox inherits this process's stdout and stderr.
		dbus.NotifyOrLog("qubesome profile error", werr.Error())

		return werr
	}

	slog.Debug("profile has gone, stopping", "profile", profile.Name)

	return nil
}

// awaitSandbox blocks until the profile's sandbox exits, and reports a
// non-zero exit as an error.
//
// The sandbox is the profile, so its exit status is the profile's own. A
// compositor that dies during startup was logged here and the start still
// returned nil, so a profile that never came up looked like a clean run to
// the caller and to the shell.
//
// Waiting is also what keeps the start alive exactly as long as the
// profile. The host serves the profile's inception socket from here, so
// returning any earlier would take the socket down under a running
// profile.
func awaitSandbox(name string, cmd *execabs.Cmd) error {
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("profile %q sandbox exited: %w", name, err)
	}

	return nil
}

func proceed(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s (Y/N): ", prompt)
		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input, please try again.")
			continue
		}

		input = strings.TrimSpace(input)
		if strings.EqualFold(input, "Y") {
			return true
		} else if strings.EqualFold(input, "N") {
			return false
		}
		fmt.Println("Invalid input. Please enter 'Y' or 'N'.")
	}
}

func createMagicCookie(profile *types.Profile) error {
	serverPath, err := files.ServerCookiePath(profile.Name)
	if err != nil {
		return err
	}
	workloadPath, err := files.ClientCookiePath(profile.Name)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(serverPath), files.DirMode)
	if err != nil {
		return err
	}

	server, err := os.OpenFile(serverPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to create server cookie %q", serverPath)
	}
	defer server.Close()

	client, err := os.OpenFile(workloadPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to create workload cookie %q", workloadPath)
	}
	defer client.Close()

	xauthority := os.Getenv("XAUTHORITY")
	if xauthority == "" {
		xauthority = os.ExpandEnv("${HOME}/.XAUTHORITY")
	}

	slog.Debug("opening parent xauthority", "path", xauthority)
	parent, err := os.Open(xauthority) //nolint:gosec // G703: path is from trusted env var
	if err != nil {
		slog.Debug("failed to open parent xauthority", "error", err)
		return err
	}
	defer parent.Close()

	return xauth.AuthPair(profile.Display, parent, server, client)
}

// sandboxEnv returns the environment of the profile sandbox, without the
// session specific variables the caller adds.
//
// The image environment comes first. Container runners applied it
// implicitly and bwrap does not, so without it the profile loses the PATH
// its own binaries are on.
//
// HOME and USER follow it rather than come from it. bwrap sets neither,
// and the image is not a reliable source: it only carries what its build
// happened to leave behind. Without HOME every X client resolves
// ~/.Xauthority to nowhere and cannot authenticate to the nested display.
// The values are profileHome and profileUser, which is where the cookies
// and the desktop files are mounted, so the environment and the mount
// list cannot disagree. bwrap applies --setenv in order, so these are the
// values that survive.
func sandboxEnv(bundle images.Bundle, ca, cert, key []byte) []string {
	const extra = 6

	env := make([]string, 0, len(bundle.Env)+extra)
	env = append(env, bundle.Env...)

	return append(env,
		"HOME="+profileHome,
		"USER="+profileUser,
		"DISPLAY="+os.Getenv("DISPLAY"),
		"Q_MTLS_CA="+string(ca),
		"Q_MTLS_CERT="+string(cert),
		"Q_MTLS_KEY="+string(key),
	)
}

// shellQuote renders args as a single line that a shell will split back
// into exactly these arguments.
//
// The profile's window manager is one argument that usually contains
// spaces, so joining on a space would print a line that runs a different
// command from the one qubesome runs.
func shellQuote(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, a := range args {
		quoted = append(quoted, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}

	return strings.Join(quoted, " ")
}

func createNewDisplay(bundle images.Bundle, ca, cert, key []byte, profile *types.Profile, display string, interactive bool, cfg *types.Config) (*execabs.Cmd, error) {
	res, err := resolution.Primary()
	if err != nil {
		return nil, err
	}

	// The display stack runs from the qubesome binary already mounted in
	// the profile, so the window manager reaches Xwayland as separate
	// arguments rather than through a shell.
	command := files.InProfileBinary
	cArgs := []string{
		"profile-display",
		"--display", display,
		"--geometry", res,
		"--auth", profileHome + "/.Xserver",
		"--wm", profile.WindowManager,
	}
	if profile.XephyrArgs != "" {
		cArgs = append(cArgs, "--extra", profile.XephyrArgs)
	}
	if profile.Fullscreen {
		cArgs = append(cArgs, "--fullscreen")
	}

	server, err := files.ServerCookiePath(profile.Name)
	if err != nil {
		return nil, err
	}
	workload, err := files.ClientCookiePath(profile.Name)
	if err != nil {
		return nil, err
	}

	// If no server cookie is found or it is empty, fail safe.
	if fi, err := os.Stat(server); err != nil || fi.Size() == 0 {
		return nil, fmt.Errorf("server cookie %q is missing or empty", server)
	}

	// The qubesome binary is bind-mounted into the sandbox and is also
	// what runs as its init, so a profile cannot start without it.
	binPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get qubesome binary path: %w", err)
	}

	socket, err := files.SocketPath(profile.Name)
	if err != nil {
		return nil, err
	}

	t := time.Now().Add(3 * time.Second)
	for {
		fi, err := os.Stat(socket)
		if err == nil {
			// The path is about to be mounted into every workload of
			// this profile and used as a socket. Anything else there
			// fails later, and further from the cause.
			if fi.Mode().Type()&os.ModeSocket == 0 {
				return nil, fmt.Errorf("%q is not a socket: %s", socket, fi.Mode().Type())
			}

			break
		}

		// Only a missing socket means it may still be on its way. Any
		// other error, a permission one for instance, would otherwise
		// be reported three seconds later as a timeout.
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to stat socket %q: %w", socket, err)
		}

		if t.Before(time.Now()) {
			return nil, fmt.Errorf("timed out waiting for socket to be created")
		}

		// Without this the loop spins on os.Stat for the whole timeout,
		// pegging a core whenever the socket takes a moment to appear.
		time.Sleep(10 * time.Millisecond)
	}

	x11Dir := "/tmp/.X11-unix"
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		fmt.Println("\033[33mWARN: Running qubesome in WSL is experimental. Some features may not work as expected.\033[0m")
		fp, err := filepath.EvalSymlinks(x11Dir)
		if err != nil {
			return nil, fmt.Errorf("failed to eval symlink: %w", err)
		}
		x11Dir = fp
	}

	// Write the machine-id file regardless of the profile using host dbus or not,
	// as this will enable workloads to use either approach.
	machineIDPath := filepath.Join(files.ProfileDir(profile.Name), "machine-id")
	err = writeMachineID(machineIDPath)
	if err != nil {
		return nil, fmt.Errorf("failed to write machine-id: %w", err)
	}

	userDir, err := files.IsolatedRunUserPath(profile.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get isolated <qubesome>/user path: %w", err)
	}
	err = setupRunUserDir(userDir)
	if err != nil {
		return nil, err
	}

	// Workload shared memory lives beside the profile's runtime dir rather
	// than inside it, because that dir is mounted into every workload.
	// EnsureMappedDir creates one workload's directory, not this parent.
	workloadShm := filepath.Join(files.ProfileDir(profile.Name), "shm")
	if err := os.MkdirAll(workloadShm, files.DirMode); err != nil {
		return nil, fmt.Errorf("failed to create workload shm dir: %w", err)
	}

	err = setupAppsDir(profile, cfg)
	if err != nil {
		return nil, err
	}

	mounts := []sandbox.Mount{
		{Src: "/etc/localtime", Dst: "/etc/localtime", ReadOnly: true},
		{Src: x11Dir, Dst: "/tmp/.X11-unix"},
		{Src: socket, Dst: "/tmp/qube.sock", ReadOnly: true},
		{Src: server, Dst: profileHome + "/.Xserver"},
		{Src: workload, Dst: profileHome + "/.Xauthority"},
		{Src: binPath, Dst: files.InProfileBinary, ReadOnly: true},
	}

	for _, p := range profile.Paths {
		p = env.Expand(p)

		parts := strings.Split(p, ":")
		// Mapped dirs are created upfront, otherwise they would be
		// created owned by root.
		if err := files.EnsureMappedDir(parts[0]); err != nil {
			fmt.Printf("\033[33mWARN: skipping mapped dir %s: %v.\033[0m\n", parts[0], err)
			continue
		}

		m := sandbox.Mount{Src: parts[0], Dst: parts[0]}
		if len(parts) > 1 {
			m.Dst = parts[1]
		}
		m.ReadOnly = len(parts) > 2 && parts[2] == "ro"

		mounts = append(mounts, m)
	}

	mounts = append(mounts, sandbox.Mount{Src: filepath.Join(userDir, "shm"), Dst: "/dev/shm"})

	if profile.Dbus {
		mounts = append(mounts,
			sandbox.Mount{Src: "/run/dbus/system_bus_socket", Dst: "/run/dbus/system_bus_socket"},
			sandbox.Mount{Src: "/etc/machine-id", Dst: "/etc/machine-id", ReadOnly: true},
		)
	} else {
		mounts = append(mounts,
			sandbox.Mount{Src: userDir, Dst: "/run/user/1000"},
			sandbox.Mount{Src: machineIDPath, Dst: "/etc/machine-id", ReadOnly: true},
		)
	}

	mounts = append(mounts,
		sandbox.Mount{
			Src:      filepath.Join(files.ProfileDir(profile.Name), "applications"),
			Dst:      profileHome + "/.local/share/applications",
			ReadOnly: true,
		},
		sandbox.Mount{
			Src:      filepath.Join(files.ProfileDir(profile.Name), "icons"),
			Dst:      profileHome + "/.local/share/icons",
			ReadOnly: true,
		},
	)

	// profile.Devices is deliberately absent. It is the allowlist that
	// bounds what a workload may ask for, applied by types.ApplyProfile
	// when a workload starts, and it was never handed to the profile
	// container itself. The display server needs the render nodes and,
	// with GPU passthrough on, the GPU nodes below.
	devices := []string{"/dev/dri"}

	if profile.Gpus != "" {
		nodes, gpuMounts, err := gpu.SandboxEdits("/")
		if err != nil {
			return nil, fmt.Errorf("failed to resolve GPU devices: %w", err)
		}
		for _, n := range nodes {
			devices = append(devices, n.Path)
		}
		for _, m := range gpuMounts {
			mounts = append(mounts, sandbox.Mount{
				Src:      m.HostPath,
				Dst:      m.ContainerPath,
				ReadOnly: true,
			})
		}
	}

	senv := sandboxEnv(bundle, ca, cert, key)

	// The profile runs its own compositor, so it needs nothing from the
	// host session beyond the display socket mounted above. The session
	// type is reported as X11 because the window manager runs on Xwayland
	// regardless of what the host session is.
	senv = append(senv, "XDG_SESSION_TYPE=X11")

	initArgs := []string{files.ShBinary}
	if interactive {
		fmt.Println("To manually start the display:")
		fmt.Printf("\t%s %s\n", command, shellQuote(cArgs))
	} else {
		initArgs = append([]string{command}, cArgs...)

		fmt.Printf("INFO: profile %s is on display :%d. On an X11 host its "+
			"window manager sees these keys first, so bind a passthrough mode "+
			"there for a better experience.\n",
			profile.Name, profile.Display)
	}

	spec := sandbox.Spec{
		Rootfs:      bundle.Rootfs,
		Hostname:    fmt.Sprintf(ContainerNameFormat, profile.Name),
		UID:         bundle.UID,
		GID:         bundle.GID,
		Net:         sandbox.NetNone,
		Seccomp:     !profile.SeccompUnconfined,
		Interactive: interactive,
		// The profile sandbox runs a compositor, an X server and a
		// window manager, none of which nest a sandbox of their own.
		DisableUserns: true,
		Env:           senv,
		Devices:       devices,
		Mounts:        mounts,
		Args:          initArgs,
		Cwd:           bundle.Cwd,
	}

	// os/exec numbers ExtraFiles from descriptor 3 upwards in the child.
	const firstExtraFD = 3

	var extra []*os.File
	seccompFD := -1

	if spec.Seccomp {
		filter, err := seccomp.MemFD()
		if err != nil {
			return nil, err
		}
		defer filter.Close()

		seccompFD = firstExtraFD + len(extra)
		extra = append(extra, filter)
	}

	args, err := sandbox.Args(spec, seccompFD)
	if err != nil {
		return nil, err
	}

	slog.Debug("exec", "binary", files.BwrapBinary, "args", container.RedactEnvArgs(args))

	// The options carry the mTLS private key through --setenv, and a
	// command line is world readable through /proc. Only the reference to
	// the descriptor holding them, and the command, stay on it.
	outer, packed, err := sandbox.PackArgs(spec, args, firstExtraFD+len(extra))
	if err != nil {
		return nil, err
	}
	defer packed.Close()

	extra = append(extra, packed)

	cmd := execabs.Command(files.BwrapBinary, outer...) //nolint:gosec // the arguments are built from the profile config.
	cmd.ExtraFiles = extra
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if interactive {
		cmd.Stdin = os.Stdin
	}

	if err := storeMtlsData(profile.Name, string(ca), string(cert), string(key)); err != nil {
		slog.Error("failed storing mtls data", "error", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start profile sandbox: %w", err)
	}

	return cmd, nil
}

func storeMtlsData(profile, ca, cert, key string) error {
	ks := keyring.New(profile, backend.New())
	if err := ks.Set(keyring.MtlsCA, ca); err != nil {
		return err
	}

	if err := ks.Set(keyring.MtlsClientCert, cert); err != nil {
		return err
	}

	if err := ks.Set(keyring.MtlsClientKey, key); err != nil {
		return err
	}
	return nil
}

func deleteMtlsData(profile string) error {
	ks := keyring.New(profile, backend.New())
	if err := ks.Delete(keyring.MtlsCA); err != nil {
		return err
	}
	if err := ks.Delete(keyring.MtlsClientCert); err != nil {
		return err
	}
	if err := ks.Delete(keyring.MtlsClientKey); err != nil {
		return err
	}
	return nil
}

func setupRunUserDir(dir string) error {
	err := os.MkdirAll(dir, files.DirMode)
	if err != nil {
		return fmt.Errorf("failed to create isolated <profile>/user dir: %w", err)
	}

	shm := filepath.Join(dir, "shm")
	err = os.MkdirAll(shm, 0o777)
	if err != nil {
		return fmt.Errorf("failed to create profile shm dir: %w", err)
	}

	err = os.Chmod(shm, 0o1777)
	if err != nil {
		return fmt.Errorf("failed to chmod profile shm dir: %w", err)
	}

	return nil
}

func setupAppsDir(profile *types.Profile, cfg *types.Config) error {
	dir := files.ProfileDir(profile.Name)

	appsDir := filepath.Join(dir, "applications")
	err := os.MkdirAll(appsDir, files.DirMode)
	if err != nil {
		return fmt.Errorf("failed to create <profile>/applications dir: %w", err)
	}

	appsRoot, err := os.OpenRoot(appsDir)
	if err != nil {
		return err
	}

	iconsDir := filepath.Join(dir, "icons")
	err = os.MkdirAll(iconsDir, files.DirMode)
	if err != nil {
		return fmt.Errorf("failed to create <profile>/icons dir: %w", err)
	}

	iconsRoot, err := os.OpenRoot(iconsDir)
	if err != nil {
		return err
	}

	for _, name := range profile.Flatpaks {
		src := filepath.Join(files.FlatpakApps(), name+".desktop")
		target := filepath.Join(dir, "applications", name+".desktop")

		err = processFlatPakFile(name, src, target)
		if err != nil {
			slog.Error("failed processing flatpak desktop file", "name", name, "error", err)
			continue
		}

		src = filepath.Join(files.FlatpakIcons(), name+".svg")
		target = filepath.Join(dir, "icons", name+".svg")
		err = copyFlatPakIcon(src, target)
		if err != nil {
			slog.Error("failed copying flatpak icon", "name", name, "error", err)
		}

		slog.Debug("added flatpak workload", "name", name)
	}

	return hydrateApps(cfg, appsRoot, iconsRoot)
}

func createGenericIcon(root *os.Root) error {
	f, err := root.Create("qubesome-generic.svg")
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(qubesomeIconBlack)
	return err
}

func hydrateApps(cfg *types.Config, appsRoot, iconsRoot *os.Root) error {
	slog.Debug("hydrating profile workloads")
	wf, err := cfg.WorkloadFiles()
	if err != nil {
		return fmt.Errorf("cannot get workloads files: %w", err)
	}

	if len(wf) == 0 {
		return nil
	}

	if err = createGenericIcon(iconsRoot); err != nil {
		slog.Debug("failed to create generic icon", "error", err)
	}

	for _, fn := range wf {
		slog.Debug("profile workload", "filename", fn)
		fi, err := os.Stat(fn)
		if err != nil {
			slog.Error("cannot stat file", "file", fn, "error", err)
		}

		if !fi.Mode().IsRegular() {
			continue
		}

		data, err := os.ReadFile(fn)
		if err != nil {
			slog.Error("cannot read file", "filename", fn, "error", err)
			continue
		}

		w := types.Workload{}
		err = yaml.Unmarshal(data, &w)
		if err != nil {
			slog.Error("cannot unmarshal workload file", "filename", fn, "error", err)
			continue
		}

		w.Name = filepath.Base(strings.TrimSuffix(fn, filepath.Ext(fn)))

		if err = w.Validate(); err != nil {
			slog.Error("invalid workload", "error", err)
			continue
		}

		appFile := w.Name + ".desktop"
		f, err := appsRoot.Create(appFile)
		if err != nil {
			slog.Error("cannot create file", "filename", appFile, "error", err)
			continue
		}

		tmpl, err := template.New("app").Parse(appTemplate)
		if err != nil {
			f.Close()

			slog.Error("cannot create template", "error", err)
			continue
		}
		err = tmpl.Execute(f, w)
		if err != nil {
			slog.Error("cannot execute template", "error", err)
		}
		f.Close()
	}
	return nil
}

func copyFlatPakIcon(sourcePath, destPath string) error {
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	err = destFile.Sync()
	if err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	return nil
}

func processFlatPakFile(workload, src, target string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("failed to create new file: %w", err)
	}
	defer destFile.Close()

	scanner := bufio.NewScanner(srcFile)
	writer := bufio.NewWriter(destFile)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Exec=") {
			line = fmt.Sprintf("Exec=/bin/sh -c '/usr/local/bin/qubesome flatpak run \"%s\"'", workload)
		} else if strings.HasPrefix(line, "Icon=") {
			line = fmt.Sprintf("Icon=~/.local/share/icons/%s.svg", workload)
		}

		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return fmt.Errorf("failed to write to destination file: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading source file: %w", err)
	}

	err = writer.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

func writeMachineID(path string) error {
	newUUID := uuid.New()
	uuidString := strings.ReplaceAll(newUUID.String(), "-", "")

	err := os.WriteFile(path, []byte(uuidString), files.FileMode)
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", path, err)
	}
	return nil
}
