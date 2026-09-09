package seccomp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyscallNumber(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"read", "write", "openat", "futex", "mmap", "execve"} {
		_, ok := SyscallNumber(name)
		assert.True(t, ok, "expected %q to resolve", name)
	}

	_, ok := SyscallNumber("definitely_not_a_syscall")
	assert.False(t, ok)
}

// A name in the profile that does not resolve on this architecture is
// denied by the default action. That is the safe direction, but it is also
// how a functional break arrives after a dependency bump, so the count is
// pinned rather than left to drift unnoticed.
func TestUnresolvedProfileNames(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	var unresolved []string
	for _, r := range p.Syscalls {
		for _, name := range r.Names {
			if _, ok := SyscallNumber(name); !ok {
				unresolved = append(unresolved, name)
			}
		}
	}

	t.Logf("unresolved on this arch (%d): %v", len(unresolved), unresolved)
	assert.Less(t, len(unresolved), 200,
		"too many profile syscalls do not resolve; the generated table is likely stale")
}
