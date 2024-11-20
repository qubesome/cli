package qubesome

import (
	"errors"
	"fmt"

	"github.com/qubesome/cli/internal/types"
)

const (
	configExtension = "yaml"
)

var (
	ErrWorkloadConfigNotFound = errors.New("workload config file not found")
	ErrProfileDirNotExist     = errors.New("profile dir does not exist")
)

type Qubesome struct {
	runner func(in WorkloadInfo, runnerOverride string) error
}

func New() *Qubesome {
	q := &Qubesome{}

	q.runner = runner

	return q
}

type WorkloadInfo struct {
	Name    string
	Profile string
	Path    string

	// Args provides additional args to the default command on the target workload
	Args   []string
	Config *types.Config
}

func (w *WorkloadInfo) Validate() error {
	if w.Config == nil {
		return fmt.Errorf("no config found")
	}
	return nil
}
