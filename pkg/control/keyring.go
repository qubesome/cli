package control

import "encoding/base64"

// KeyringReference identifies one host-store entry. It carries no credential.
type KeyringReference struct {
	Service string `yaml:"service"`
	Account string `yaml:"account"`
}

// KeyringID encodes both components without delimiter collisions.
func KeyringID(service, account string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(service)) + ":" +
		base64.RawURLEncoding.EncodeToString([]byte(account))
}
