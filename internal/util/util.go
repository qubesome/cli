package util

import (
	"fmt"
	"os"
	"path/filepath"
)

type QubesomePath int

const (
	QubesomeDir QubesomePath = iota
	WorkloadsDir
	ProfilesDir
)

var (
	ErrQubesomePathNotDir = fmt.Errorf("qubesome path must be a dir")
)

func Path(in QubesomePath) (string, error) {
	var p string

	switch in {
	case QubesomeDir:
		p = ".qubesome"
	case WorkloadsDir:
		p = filepath.Join(".qubesome", "workloads")
	case ProfilesDir:
		p = filepath.Join(".qubesome", "profiles")
	default:
		return "", fmt.Errorf("unsupported path")
	}

	d, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	qd := filepath.Join(d, p)
	fs, err := os.Stat(qd)
	if err != nil {
		return "", err
	}

	if !fs.IsDir() {
		return "", ErrQubesomePathNotDir
	}

	return qd, nil
}
