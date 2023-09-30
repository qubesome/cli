package qubesome

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/qubesome/qubesome-cli/internal/types"
)

const (
	qubesomeDir     = ".qubesome"
	workloadsDir    = "workloads"
	profilesDir     = "profiles"
	configExtension = "yaml"
)

var (
	ErrQubesomeHomeNotDir     = fmt.Errorf("qubesome home path must be a dir")
	ErrWorkloadConfigNotFound = fmt.Errorf("workload config file not found")
	ErrProfileDirNotExist     = fmt.Errorf("profile dir does not exist")
)

type Qubesome struct {
	Config *types.Config

	runner func(in WorkloadInfo) error
}

func New() *Qubesome {
	q := &Qubesome{}

	q.runner = q.Run

	return q
}

var runner func(in WorkloadInfo) error

func qubesomeHome() (string, error) {
	d, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	qd := filepath.Join(d, qubesomeDir)
	fs, err := os.Stat(qd)
	if err != nil {
		return "", err
	}

	if !fs.IsDir() {
		return "", ErrQubesomeHomeNotDir
	}

	return qd, nil
}

type WorkloadInfo struct {
	Name    string
	Profile string

	// Args provides additional args to the default command on the target workload
	Args []string
}

func (w *WorkloadInfo) Validate() error {
	// TODO: Name/Profile
	return nil
}
