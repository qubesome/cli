package doctor

import (
	"fmt"
	"path/filepath"

	"github.com/qubesome/cli/internal/files"
	"github.com/qubesome/cli/internal/types"
)

// source is where a profile's configuration was read from, and what its
// paths resolve against.
//
// Everything a profile owns descends from one of these, so getting them
// wrong sends every later check looking somewhere nothing was ever
// written, and the report then describes a machine nobody is running.
type source struct {
	// config is the config file that was read.
	config string

	// root is what a profile's path descends from, and what the run
	// command expands ${GITDIR} to.
	root string

	// gitDir is the repository the config was sourced from, which is
	// what a start expands ${GITDIR} to. It is the root when the config
	// is not in a repository.
	gitDir string
}

// resolveSource works out where a profile's configuration came from.
//
// A started profile is reached through <qubesome>/run/<name>.config, a
// symlink into the directory the config was sourced from. LoadConfig
// takes the root dir from the path it opened, so a config reached that
// way reports the run dir as its root, and a profile's path then resolves
// under <qubesome>/run rather than under the repository. Resolving the
// link gives the real root back.
func resolveSource(env Env, cfg *types.Config, profileName string) source {
	// A config that did not load is a finding of its own, reported by the
	// config check. Resolving nothing keeps this from being the place it
	// surfaces.
	if cfg == nil {
		return source{}
	}

	root := cfg.RootDir

	if cfg.RootDir == files.RunUserQubesome() && profileName != "" {
		if target, err := env.Readlink(files.ProfileConfig(profileName)); err == nil {
			root = filepath.Dir(target)

			return source{config: target, root: root, gitDir: gitDir(env, root)}
		}
	}

	return source{
		config: filepath.Join(root, "qubesome.config"),
		root:   root,
		gitDir: gitDir(env, root),
	}
}

// gitDir returns the repository dir at or above root.
//
// A start sets ${GITDIR} to the repository it sourced the config from,
// not to the directory the config sits in, and the two differ whenever
// the config lives in a subdirectory of the repository. Walking up to the
// repository recovers what a start would have registered. A config that
// is not in a repository expands ${GITDIR} to its own root, which is what
// the run command does.
func gitDir(env Env, root string) string {
	dir := root

	for {
		if _, err := env.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return root
		}

		dir = parent
	}
}

// runnerFor resolves the container runner the way a profile start
// resolves it: the -runner flag wins, and a profile names its own runner
// otherwise. A host with both runners installed answers about the wrong
// one when the profile's choice is ignored, which reads as a running
// profile not running.
func runnerFor(runner string, profile types.Profile) string {
	if runner == "" && profile.Runner != "" {
		return profile.Runner
	}

	return runner
}

// checkProfileSource reports where the profile's things are.
//
// It never fails, because it is not a test of anything. It is here
// because every other check in the section is relative to these paths,
// and a report that says something is missing without saying where it
// looked cannot be acted on. It also makes the two ways a config is
// reached, directly and through a started profile's symlink, visible
// rather than something to infer from the paths in the other checks.
func checkProfileSource(src source) Check {
	detail := fmt.Sprintf("config %s, root %s", src.config, src.root)
	if src.gitDir != src.root {
		detail += fmt.Sprintf(", ${GITDIR} %s", src.gitDir)
	}

	return Check{
		Name:   "profile source",
		Status: OK,
		Detail: detail,
	}
}
