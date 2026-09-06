package seccomp

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/bpf"
)

// data builds a seccomp_data buffer for the VM. The VM reads big-endian,
// unlike the kernel, which reads the struct natively.
func data(nr uint32, arch uint32, args ...uint64) []byte {
	b := make([]byte, 64)
	binary.BigEndian.PutUint32(b[0:], nr)
	binary.BigEndian.PutUint32(b[4:], arch)
	for i, a := range args {
		if i >= 6 {
			break
		}
		binary.BigEndian.PutUint32(b[16+8*i:], uint32(a&math.MaxUint32))
		binary.BigEndian.PutUint32(b[20+8*i:], uint32(a>>32))
	}
	return b
}

func vm(t *testing.T) *bpf.VM {
	t.Helper()

	p, err := Load()
	require.NoError(t, err)

	insns, err := p.Program()
	require.NoError(t, err)

	m, err := bpf.NewVM(insns)
	require.NoError(t, err)
	return m
}

func run(t *testing.T, m *bpf.VM, name string, args ...uint64) uint32 {
	t.Helper()

	nr, ok := SyscallNumber(name)
	require.True(t, ok, "syscall %q does not resolve on this arch", name)

	return ret(t, m, data(nr, auditArch, args...))
}

// ret runs the program and narrows the VM's int result to the 32 bit
// seccomp return value it carries. The bounds are asserted first, so the
// mask cannot hide a result that does not fit.
func ret(t *testing.T, m *bpf.VM, in []byte) uint32 {
	t.Helper()

	out, err := m.Run(in)
	require.NoError(t, err)
	require.GreaterOrEqual(t, out, 0)
	require.LessOrEqual(t, out, math.MaxUint32)

	return uint32(out & math.MaxUint32)
}

func TestProgramAllowsOrdinarySyscalls(t *testing.T) {
	t.Parallel()

	m := vm(t)
	for _, name := range []string{"read", "write", "openat", "mmap", "futex", "execve"} {
		assert.Equal(t, uint32(retAllow), run(t, m, name), "expected %q to be allowed", name)
	}
}

func TestProgramDeniesPrivilegedSyscalls(t *testing.T) {
	t.Parallel()

	m := vm(t)
	for _, name := range []string{"init_module", "chroot", "acct", "iopl", "settimeofday", "bpf"} {
		assert.Equal(t, uint32(retErrno|1), run(t, m, name), "expected %q to return EPERM", name)
	}
}

func TestProgramDeniesUnlistedSyscallsWithDefaultErrno(t *testing.T) {
	t.Parallel()

	m := vm(t)
	assert.Equal(t, uint32(retErrno|38), ret(t, m, data(9999, auditArch)),
		"unlisted syscalls must return ENOSYS")
}

func TestProgramKillsOnForeignArch(t *testing.T) {
	t.Parallel()

	m := vm(t)
	nr, ok := SyscallNumber("read")
	require.True(t, ok)

	// AUDIT_ARCH_ARM
	assert.Equal(t, uint32(retKillProcess), ret(t, m, data(nr, 0x40000028)))
}

func TestProgramPersonalityIsConditional(t *testing.T) {
	t.Parallel()

	m := vm(t)
	for _, v := range []uint64{0, 8, 0x20000, 0x20008, 0xffffffff} {
		assert.Equal(t, uint32(retAllow), run(t, m, "personality", v),
			"expected personality(%#x) to be allowed", v)
	}
	assert.Equal(t, uint32(retErrno|38), run(t, m, "personality", 1),
		"expected personality(1) to fall through to the default action")
}

func TestProgramSocketDeniesNetlinkAudit(t *testing.T) {
	t.Parallel()

	m := vm(t)
	// socket(AF_NETLINK, _, NETLINK_AUDIT) is denied with EINVAL, and the
	// first matching rule wins, so it must beat the allow rules below it.
	assert.Equal(t, uint32(retErrno|22), run(t, m, "socket", 16, 0, 9))
	assert.Equal(t, uint32(retAllow), run(t, m, "socket", 2, 0, 0))
	assert.Equal(t, uint32(retAllow), run(t, m, "socket", 16, 0, 0))
}

func TestProgramFitsInstructionLimit(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	insns, err := p.Program()
	require.NoError(t, err)

	raw, err := bpf.Assemble(insns)
	require.NoError(t, err)
	assert.Less(t, len(raw), 4096, "filter exceeds the kernel instruction limit")
	t.Logf("filter is %d instructions", len(raw))
}

func TestProgramSkipsCapabilityGatedAllowRules(t *testing.T) {
	t.Parallel()

	m := vm(t)
	// Every one of these is allowed by a rule group gated on a capability
	// the sandbox never holds, and denied by the group that follows it. The
	// gated allow must not be emitted, or the denial never gets reached.
	for _, name := range []string{
		"open_by_handle_at", "sethostname", "setdomainname",
		"quotactl", "lookup_dcookie", "perf_event_open", "delete_module",
		"finit_module", "query_module", "kcmp", "process_madvise",
		"ioperm", "clock_settime", "vhangup",
	} {
		assert.Equal(t, uint32(retErrno|1), run(t, m, name),
			"expected %q to return EPERM", name)
	}

	// setns is the one syscall the profile both allows unconditionally and
	// denies under a capability exclusion. The unconditional allow comes
	// first, so it wins. The kernel still requires CAP_SYS_ADMIN in the
	// target namespace, which the sandbox does not have.
	assert.Equal(t, uint32(retAllow), run(t, m, "setns"))
}

// expectedAction evaluates the profile directly, so the program can be
// checked against it rather than against a handful of hand written cases.
// It shares applies and action with the emitter, so what it independently
// checks is the jump arithmetic, which is where the risk lives.
func expectedAction(t *testing.T, p *Profile, nr uint32, args [6]uint64) uint32 {
	t.Helper()

	for _, r := range p.Syscalls {
		if !applies(r) || !names(r, nr) {
			continue
		}

		matched := true
		for _, a := range r.Args {
			switch a.Op {
			case opEqual:
				matched = args[a.Index] == a.Value
			case opNotEqual:
				matched = args[a.Index] != a.Value
			default:
				t.Fatalf("unsupported operator %q", a.Op)
			}
			if !matched {
				break
			}
		}
		if !matched {
			continue
		}

		ret, err := action(r.Action, r.ErrnoRet)
		require.NoError(t, err)
		return ret
	}

	def, err := action(p.DefaultAction, p.DefaultErrnoRet)
	require.NoError(t, err)
	return def
}

func names(r Rule, nr uint32) bool {
	for _, n := range r.Names {
		if v, ok := SyscallNumber(n); ok && v == nr {
			return true
		}
	}
	return false
}

func TestProgramMatchesTheProfileForEverySyscall(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	m := vm(t)

	// The last two vectors set bits above the low word, which the profile
	// never matches on, so they check that the emitter compares both halves
	// of a 64 bit argument.
	vectors := [][6]uint64{
		{},
		{16, 0, 9},
		{16, 0, 0},
		{2, 1, 0},
		{0x20008, 0, 0},
		{0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff, 0xffffffff},
		{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
		{1 << 40, 1 << 40, 1 << 40, 1 << 40, 1 << 40, 1 << 40},
	}

	seen := make(map[uint32]bool)
	for _, r := range p.Syscalls {
		for _, name := range r.Names {
			nr, ok := SyscallNumber(name)
			if !ok || seen[nr] {
				continue
			}
			seen[nr] = true

			for _, v := range vectors {
				assert.Equal(t, expectedAction(t, p, nr, v),
					ret(t, m, data(nr, auditArch, v[:]...)),
					"syscall %s with args %v", name, v)
			}
		}
	}
	assert.NotEmpty(t, seen)
	t.Logf("checked %d syscalls against %d argument vectors", len(seen), len(vectors))
}

func BenchmarkProgram(b *testing.B) {
	p, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	insns, err := p.Program()
	if err != nil {
		b.Fatal(err)
	}
	m, err := bpf.NewVM(insns)
	if err != nil {
		b.Fatal(err)
	}
	nr, _ := SyscallNumber("write")
	in := data(nr, auditArch)

	b.ResetTimer()
	for b.Loop() {
		if _, err := m.Run(in); err != nil {
			b.Fatal(err)
		}
	}
}
