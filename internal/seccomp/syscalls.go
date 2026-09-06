package seccomp

import "errors"

//go:generate go run ../../hack/syscallgen

// ErrUnsupportedArch is returned when no syscall table was generated for
// the running architecture. Qubesome fails closed rather than starting a
// profile with no filter. Set seccompUnconfined on the profile to opt out
// deliberately.
var ErrUnsupportedArch = errors.New("no seccomp syscall table for this architecture")

// SyscallNumber returns the number of a syscall on the running
// architecture. Names absent from the table do not exist here, and the
// profile's default action applies to them.
func SyscallNumber(name string) (uint32, bool) {
	n, ok := syscallNumbers[name]
	return n, ok
}
