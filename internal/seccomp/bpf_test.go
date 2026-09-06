package seccomp

import (
	"encoding/binary"
	"math"
	"slices"
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

	// socket also appears in an ERRNO group, but that group carries argument
	// conditions, so the outright denial pre-pass leaves these three
	// verdicts exactly as profile order produces them.
	p, err := Load()
	require.NoError(t, err)
	assert.False(t, deniedOutright(p)["socket"])
}

// TCGETS is an ordinary terminal request, so it stands for everything the
// broad allow group still lets through.
func TestProgramDeniesTerminalInjectionIoctls(t *testing.T) {
	t.Parallel()

	const tcgets = 0x5401

	m := vm(t)
	for _, request := range []uint64{tiocsti, tioclinux} {
		assert.Equal(t, uint32(retErrno|eperm), run(t, m, "ioctl", 0, request),
			"expected ioctl(_, %#x) to return EPERM", request)
	}

	assert.Equal(t, uint32(retAllow), run(t, m, "ioctl", 0, tcgets))

	// The request is the second argument, so the same value in the first
	// one must not match.
	assert.Equal(t, uint32(retAllow), run(t, m, "ioctl", tiocsti, tcgets))
}

// The kernel declares the request as an unsigned int and truncates it, so
// a caller can set any bit above the low word and still reach TIOCSTI. An
// equality over the whole register would let that through, which is why the
// rule masks. Verified on this host: ioctl on a pty with
// 0xffffffff00005401 performs TCGETS and returns 0.
func TestProgramDeniesTerminalInjectionIoctlsWithHighBitsSet(t *testing.T) {
	t.Parallel()

	m := vm(t)
	for _, request := range []uint64{tiocsti, tioclinux} {
		for _, high := range []uint64{1 << 32, 1 << 40, 0xffffffff << 32} {
			got := run(t, m, "ioctl", 0, request|high)
			assert.Equal(t, uint32(retErrno|eperm), got,
				"expected ioctl(_, %#x) to return EPERM", request|high)
		}
	}
}

// Value is the mask and ValueTwo the datum, and both words are masked
// before they are compared.
func TestCompareMaskedEqual(t *testing.T) {
	t.Parallel()

	insns, err := rule(1, retAllow, []Arg{{
		Index:    0,
		Value:    0xff00ff00ff00ff00,
		ValueTwo: 0xaa00bb00cc00dd00,
		Op:       opMaskedEqual,
	}})
	require.NoError(t, err)

	// A rule fragment expects the syscall number already in A, and falls
	// through to the next rule when the condition fails.
	prog := make([]bpf.Instruction, 0, len(insns)+2)
	prog = append(prog, bpf.LoadAbsolute{Off: offNr, Size: 4})
	prog = append(prog, insns...)
	prog = append(prog, bpf.RetConstant{Val: retErrno})

	m, err := bpf.NewVM(prog)
	require.NoError(t, err)

	for _, arg := range []uint64{
		0xaa00bb00cc00dd00,
		0xaaffbbffccffddff,
		0xaa11bb22cc33dd44,
	} {
		assert.Equal(t, uint32(retAllow), ret(t, m, data(1, auditArch, arg)),
			"expected %#x to match", arg)
	}

	for _, arg := range []uint64{
		0,
		0xab00bb00cc00dd00,
		0xaa00bb00cc00de00,
	} {
		assert.Equal(t, uint32(retErrno), ret(t, m, data(1, auditArch, arg)),
			"expected %#x not to match", arg)
	}
}

// The vendored profile allows ioctl with no argument conditions, so a
// denial appended after it would never be reached, and the outright denial
// pre-pass does not cover argument carrying rules.
func TestInjectedRulesPrecedeTheProfile(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	assert.False(t, deniedOutright(p)["ioctl"])

	for _, r := range p.Syscalls {
		if slices.Contains(r.Names, "ioctl") {
			assert.Equal(t, actionAllow, r.Action)
			assert.Empty(t, r.Args)
		}
	}
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
		"quotactl", "quotactl_fd", "lookup_dcookie", "delete_module",
		"finit_module", "query_module", "kcmp", "process_madvise",
		"ioperm", "clock_settime", "vhangup",
	} {
		assert.Equal(t, uint32(retErrno|1), run(t, m, name),
			"expected %q to return EPERM", name)
	}
}

func TestProgramDropsAnAllowCoveredByAnOutrightDenial(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	// setns is the only syscall the profile both allows with no argument
	// conditions and denies with none. Profile order alone would let the
	// allow win, which would leave the filter permitting a syscall the
	// policy denies without CAP_SYS_ADMIN.
	denied := deniedOutright(p)
	assert.True(t, denied["setns"])
	assert.Equal(t, uint32(retErrno|1), run(t, vm(t), "setns"))

	// Argument partitioned groups stay out of the pre-pass, so the socket
	// rules keep resolving by profile order.
	assert.False(t, denied["socket"])
	assert.False(t, denied["personality"])
}

// expectedAction evaluates the profile directly, so the program can be
// checked against it rather than against a handful of hand written cases.
// It shares applies and action with the emitter, so what it independently
// checks is the jump arithmetic, which is where the risk lives.
//
// The injected rules are part of what the emitter walks, so they are part
// of what this walks. Leaving them out would only make the two disagree
// about ioctl.
func expectedAction(t *testing.T, p *Profile, denied map[string]bool, nr uint32, args [6]uint64) uint32 {
	t.Helper()

	for _, r := range slices.Concat(injected(), p.Syscalls) {
		if !applies(r) {
			continue
		}

		name, ok := nameFor(r, nr)
		if !ok {
			continue
		}
		if len(r.Args) == 0 && r.Action == actionAllow && denied[name] {
			continue
		}

		matched := true
		for _, a := range r.Args {
			switch a.Op {
			case opEqual:
				matched = args[a.Index] == a.Value
			case opNotEqual:
				matched = args[a.Index] != a.Value
			case opMaskedEqual:
				matched = args[a.Index]&a.Value == a.ValueTwo
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

// nameFor returns the first name in the group that resolves to nr, which is
// the name whose fragment the emitter reaches first.
func nameFor(r Rule, nr uint32) (string, bool) {
	for _, n := range r.Names {
		if v, ok := SyscallNumber(n); ok && v == nr {
			return n, true
		}
	}
	return "", false
}

func TestProgramMatchesTheProfileForEverySyscall(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	m := vm(t)
	denied := deniedOutright(p)

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
		{0, tiocsti},
		{0, tioclinux},
		{0, tiocsti | 1<<40},
		{0, 0x5401},
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
				assert.Equal(t, expectedAction(t, p, denied, nr, v),
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
