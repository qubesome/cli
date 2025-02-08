package backend

import "github.com/zalando/go-keyring"

func New() *backend {
	return &backend{}
}

type backend struct {
}

func (backend) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (backend) Set(service, user, value string) error {
	return keyring.Set(service, user, value)
}

func (backend) Delete(service, user string) error {
	return keyring.Delete(service, user)
}
