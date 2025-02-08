package keyring

import (
	"errors"
	"fmt"
	"os/user"
)

var ErrBackEndCannotBeNil = errors.New("backend cannot be nil")

// New returns a keyring frontend to manage qubesome secrets.
func New(profile string, backend Backend) *frontend {
	return &frontend{
		profile,
		backend,
	}
}

type Backend interface {
	// Set stores the value in a keyring service for user.
	Set(service, user, password string) error
	// Get returns the value stored in a keyring service for user.
	Get(service, user string) (string, error)
	// Delete removes any stored values in a keyring service.
	Delete(service, user string) error
}

type frontend struct {
	profile string
	backend Backend
}

func (k *frontend) Get(name SecretName) (string, error) {
	if k.backend == nil {
		return "", ErrBackEndCannotBeNil
	}

	u, err := user.Current()
	if err != nil {
		return "", err
	}

	svc := fmt.Sprintf("qubesome:%s:%s", k.profile, name)
	val, err := k.backend.Get(svc, u.Name)
	if err != nil {
		return "", fmt.Errorf("cannot get value for %q: %w", svc, err)
	}
	return val, nil
}

func (k *frontend) Set(name SecretName, value string) error {
	if k.backend == nil {
		return ErrBackEndCannotBeNil
	}

	u, err := user.Current()
	if err != nil {
		return err
	}

	svc := fmt.Sprintf("qubesome:%s:%s", k.profile, name)
	err = k.backend.Set(svc, u.Name, value)
	if err != nil {
		return fmt.Errorf("cannot set value for %q: %w", svc, err)
	}
	return nil
}

func (k *frontend) Delete(name SecretName) error {
	if k.backend == nil {
		return ErrBackEndCannotBeNil
	}

	u, err := user.Current()
	if err != nil {
		return err
	}

	svc := fmt.Sprintf("qubesome:%s:%s", k.profile, name)
	err = k.backend.Delete(svc, u.Name)
	if err != nil {
		return fmt.Errorf("cannot delete value for %q: %w", svc, err)
	}
	return nil
}
