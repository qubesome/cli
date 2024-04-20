package qubesome

import (
	"errors"

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
	Config *types.Config

	runner func(in WorkloadInfo) error
}

func New() *Qubesome {
	q := &Qubesome{}

	q.runner = q.Run

	return q
}

var runner func(in WorkloadInfo) error

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
