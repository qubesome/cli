//go:build linux && !amd64 && !arm64

package seccomp

const auditArch = 0

var syscallNumbers = map[string]uint32{}
