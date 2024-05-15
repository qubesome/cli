package env

import (
	"fmt"
	"os"
)

func init() { //nolint
	h, _ := os.UserHomeDir()
	_ = Update("HOME", h)
}

var mapping = map[string]string{
	"HOME":   "",
	"GITDIR": "",
}

func Update(k, v string) error {
	if _, ok := mapping[k]; ok {
		mapping[k] = v
		return nil
	}
	return fmt.Errorf("%q is not an expandable env var", k)
}

func Add(k, v string) {
	mapping[k] = v
}

func Expand(in string) string {
	return os.Expand(in, expand)
}

func expand(s string) string {
	if out, ok := mapping[s]; ok {
		return out
	}
	return ""
}
