package seccomp

import (
	"fmt"
	"math"
	"runtime"
	"slices"

	"golang.org/x/net/bpf"
)

const (
	retAllow       = 0x7fff0000
	retErrno       = 0x00050000
	retKillProcess = 0x80000000

	offNr   = 0
	offArch = 4
	offArgs = 16

	// numArgs is the length of seccomp_data.args.
	numArgs = 6
)

const (
	actionAllow = "SCMP_ACT_ALLOW"
	actionErrno = "SCMP_ACT_ERRNO"

	opEqual       = "SCMP_CMP_EQ"
	opNotEqual    = "SCMP_CMP_NE"
	opMaskedEqual = "SCMP_CMP_MASKED_EQ"
)

const (
	// eperm is the errno the vendored profile returns for the privileged
	// operations it denies, so the rules below return it too.
	eperm = 1

	// ioctlRequest is the index of the request in
	// ioctl(int fd, unsigned long request, ...), so it is the argument the
	// rules below test.
	ioctlRequest = 1

	// requestMask keeps the comparison to the bits the kernel reads. The
	// syscall declares the request as an unsigned int, so the kernel
	// truncates it, and a plain equality over the whole 64 bit register
	// would miss a request carrying any bit above the low word. Verified
	// against this host: ioctl on a pty with 0xffffffff00005401 performs
	// TCGETS and succeeds.
	requestMask = 0xffffffff

	// TIOCSTI and TIOCLINUX both push bytes into a terminal's input queue.
	// The values are from asm-generic/ioctls.h, which is the definition
	// both architectures with a syscall table here use.
	tiocsti   = 0x5412
	tioclinux = 0x541c
)

// injected are qubesome's own rules, which the emitter puts in front of the
// vendored profile.
//
// An interactive sandbox skips --new-session, so it holds the terminal that
// ran qubesome, and a process inside could use either of these requests to
// queue a command that the shell runs once qubesome exits. bubblewrap's own
// manpage warns about exactly this. Docker was not exposed the same way,
// because it attached through a pty pair of its own rather than handing the
// container the user's terminal.
//
// They live here rather than in seccomp.json because that file is upstream
// data, and a re-vendor would drop them. They have to precede the vendored
// groups: rules are emitted in profile order, the first match wins, and the
// broad allow group allows ioctl outright, so a denial behind it is
// unreachable. The deniedOutright pre-pass does not help, as it only covers
// argument free denials.
func injected() []Rule {
	rules := make([]Rule, 0, 2)

	for _, request := range []uint64{tiocsti, tioclinux} {
		errno := uint32(eperm)
		rules = append(rules, Rule{
			Names:    []string{"ioctl"},
			Action:   actionErrno,
			ErrnoRet: &errno,
			Args: []Arg{{
				Index:    ioctlRequest,
				Value:    requestMask,
				ValueTwo: request,
				Op:       opMaskedEqual,
			}},
		})
	}

	return rules
}

// Program renders the profile as a BPF program.
//
// Only the native architecture is filtered. The profile's archMap is not
// consulted, so a process making a syscall under any other ABI, a 32 bit
// i386 binary being the realistic case, is killed by the leading
// architecture guard rather than filtered. Docker would have filtered it
// against the same policy. Killing is the safer side to err on for a
// desktop sandbox and it keeps one filter to reason about.
//
// Rule groups the sandbox does not qualify for are dropped, see applies.
// An argument free allow is dropped when an argument free denial elsewhere
// in the profile covers the same syscall, see deniedOutright.
//
// Rules are emitted in profile order and the first match wins, which is
// what makes the conditional socket rules behave as written: the netlink
// audit denial precedes the allow rules that exclude it. It is also why
// qubesome's own rules go in front of the profile, see injected.
//
// The shape is a comparison chain rather than the balanced search
// libseccomp emits. No conditional jump travels further than one rule
// body, and rule rejects a body too long for an 8 bit offset, so the
// vendored profile stays far inside the range. The cost is a longer
// average walk per syscall. BenchmarkProgram times that walk through the
// x/net/bpf userspace interpreter, which is not the kernel's cost for it.
func (p *Profile) Program() ([]bpf.Instruction, error) {
	if auditArch == 0 {
		return nil, ErrUnsupportedArch
	}

	def, err := action(p.DefaultAction, p.DefaultErrnoRet)
	if err != nil {
		return nil, fmt.Errorf("default action: %w", err)
	}

	denied := deniedOutright(p)

	insns := []bpf.Instruction{
		bpf.LoadAbsolute{Off: offArch, Size: 4},
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: auditArch, SkipTrue: 1},
		bpf.RetConstant{Val: retKillProcess},
		bpf.LoadAbsolute{Off: offNr, Size: 4},
	}

	for _, r := range slices.Concat(injected(), p.Syscalls) {
		if !applies(r) {
			continue
		}

		ret, err := action(r.Action, r.ErrnoRet)
		if err != nil {
			return nil, fmt.Errorf("syscall %v: %w", r.Names, err)
		}

		for _, name := range r.Names {
			if len(r.Args) == 0 && r.Action == actionAllow && denied[name] {
				continue
			}

			nr, ok := SyscallNumber(name)
			if !ok {
				// The syscall does not exist on this architecture, so the
				// default action covers it.
				continue
			}

			frag, err := rule(nr, ret, r.Args)
			if err != nil {
				return nil, fmt.Errorf("syscall %s: %w", name, err)
			}
			insns = append(insns, frag...)
		}
	}

	return append(insns, bpf.RetConstant{Val: def}), nil
}

// deniedOutright collects the syscalls an applicable rule group denies with
// no argument conditions. The profile puts its capability overrides after
// the broad allow list, so first match wins would let the allow win for a
// syscall named in both. Only argument free groups take part, which leaves
// the argument partitioned socket rules to first match wins as written.
func deniedOutright(p *Profile) map[string]bool {
	denied := make(map[string]bool)

	for _, r := range p.Syscalls {
		if !applies(r) || len(r.Args) > 0 || r.Action != actionErrno {
			continue
		}
		for _, n := range r.Names {
			denied[n] = true
		}
	}

	return denied
}

// applies reports whether a rule group is in force for the sandbox.
//
// The profile is written for containers that may hold capabilities, so it
// pairs most privileged syscalls: one group allows them when a capability
// is held and another denies them when it is not. The sandbox runs in an
// unprivileged user namespace with an empty capability set, so a group
// gated on holding a capability never applies. Emitting those groups
// anyway would allow init_module, chroot, bpf and the rest unconditionally.
//
// A group gated on not holding a capability always applies, because no
// capability can be held, so excludes.caps needs no test here.
//
// The architecture names in the profile are Go architecture names, which is
// what containers/common compares them against.
func applies(r Rule) bool {
	if slices.Contains(r.Excludes.Arches, runtime.GOARCH) {
		return false
	}
	if len(r.Includes.Arches) > 0 && !slices.Contains(r.Includes.Arches, runtime.GOARCH) {
		return false
	}
	return len(r.Includes.Caps) == 0
}

// failJump marks a jump whose destination is the end of the rule body. The
// distance is only known once the body is complete, so it is patched by
// rule below. It is not a valid skip distance, so an unpatched one cannot
// survive unnoticed.
const failJump = 0xffffffff

// rule emits the comparison for one syscall number.
//
// Register A holds the syscall number on entry and must hold it again on
// exit, because the next rule compares against it. A rule that loads an
// argument therefore reloads the number before falling through, and that
// reload is also where a failed argument test lands.
//
// Layout of a rule with arguments:
//
//	jne nr -> end        number does not match, skip the whole body
//	<argument tests>     a failing test jumps to reload
//	ret action           every test passed
//	reload: ld [nr]      restore A for the next rule
//	end:
func rule(nr, ret uint32, args []Arg) ([]bpf.Instruction, error) {
	if len(args) == 0 {
		return []bpf.Instruction{
			bpf.JumpIf{Cond: bpf.JumpEqual, Val: nr, SkipFalse: 1},
			bpf.RetConstant{Val: ret},
		}, nil
	}

	var body []bpf.Instruction
	for _, a := range args {
		test, err := compare(a)
		if err != nil {
			return nil, err
		}
		body = append(body, test...)
	}
	body = append(body,
		bpf.RetConstant{Val: ret},
		bpf.LoadAbsolute{Off: offNr, Size: 4},
	)

	// The leading test skips the whole body, so the body has to stay within
	// reach of an 8 bit offset. The widest condition emitted here is eight
	// instructions and no rule carries more than two, so this cannot
	// trigger with the vendored profile. It guards a future one that packs
	// far more conditions into a rule.
	size := len(body)
	if size > math.MaxUint8 {
		return nil, fmt.Errorf("rule body of %d instructions exceeds the jump range", size)
	}

	// A jump at index i lands on i+1+Skip, and the reload is the last
	// instruction of the body.
	reload := size - 1
	for i, in := range body {
		j, ok := in.(bpf.Jump)
		if !ok || j.Skip != failJump {
			continue
		}

		dist := reload - i - 1
		if dist < 0 || dist > math.MaxUint8 {
			return nil, fmt.Errorf("failure jump at %d cannot reach the reload at %d", i, reload)
		}
		body[i] = bpf.Jump{Skip: uint32(dist)}
	}

	out := make([]bpf.Instruction, 0, size+1)
	out = append(out, bpf.JumpIf{Cond: bpf.JumpEqual, Val: nr, SkipFalse: uint8(size)})

	return append(out, body...), nil
}

// compare emits the test for one argument condition.
//
// A passing test falls through to the next one, and a failing test jumps to
// the reload that ends the rule body, so control reaches the next rule with
// A holding the syscall number again.
//
// Arguments are 64 bit, so both words are compared. Comparing only the low
// word would let a caller pass a value whose high bits differ and still
// match.
//
// The kernel reads seccomp_data in the host byte order, so the low word of
// an argument sits at the lower offset. Both architectures with a syscall
// table are little endian.
func compare(a Arg) ([]bpf.Instruction, error) {
	if a.Index >= numArgs {
		return nil, fmt.Errorf("argument index %d is out of range", a.Index)
	}

	lo := offArgs + 8*a.Index
	hi := lo + 4

	valLo := uint32(a.Value & math.MaxUint32)
	valHi := uint32(a.Value >> 32)

	switch a.Op {
	case opEqual:
		// Equal needs both words to match, so either mismatch fails.
		return []bpf.Instruction{
			bpf.LoadAbsolute{Off: hi, Size: 4},
			bpf.JumpIf{Cond: bpf.JumpEqual, Val: valHi, SkipTrue: 1},
			bpf.Jump{Skip: failJump},
			bpf.LoadAbsolute{Off: lo, Size: 4},
			bpf.JumpIf{Cond: bpf.JumpEqual, Val: valLo, SkipTrue: 1},
			bpf.Jump{Skip: failJump},
		}, nil
	case opMaskedEqual:
		// Value is the mask and ValueTwo the datum, which is how
		// libseccomp orders the pair and how the profile schema carries
		// it. Both words are masked and compared, so a mask that clears a
		// word makes that word's test pass unconditionally.
		maskLo := uint32(a.Value & math.MaxUint32)
		maskHi := uint32(a.Value >> 32)
		datumLo := uint32(a.ValueTwo & math.MaxUint32)
		datumHi := uint32(a.ValueTwo >> 32)

		return []bpf.Instruction{
			bpf.LoadAbsolute{Off: hi, Size: 4},
			bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: maskHi},
			bpf.JumpIf{Cond: bpf.JumpEqual, Val: datumHi, SkipTrue: 1},
			bpf.Jump{Skip: failJump},
			bpf.LoadAbsolute{Off: lo, Size: 4},
			bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: maskLo},
			bpf.JumpIf{Cond: bpf.JumpEqual, Val: datumLo, SkipTrue: 1},
			bpf.Jump{Skip: failJump},
		}, nil
	case opNotEqual:
		// Not equal needs only one word to differ, so a high word
		// mismatch skips the low word test and the failure jump with it.
		return []bpf.Instruction{
			bpf.LoadAbsolute{Off: hi, Size: 4},
			bpf.JumpIf{Cond: bpf.JumpNotEqual, Val: valHi, SkipTrue: 3},
			bpf.LoadAbsolute{Off: lo, Size: 4},
			bpf.JumpIf{Cond: bpf.JumpNotEqual, Val: valLo, SkipTrue: 1},
			bpf.Jump{Skip: failJump},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported operator %q", a.Op)
	}
}

func action(name string, errnoRet *uint32) (uint32, error) {
	switch name {
	case actionAllow:
		return retAllow, nil
	case actionErrno:
		errno := uint32(1)
		if errnoRet != nil {
			errno = *errnoRet
		}
		return retErrno | (errno & 0xffff), nil
	default:
		return 0, fmt.Errorf("unsupported action %q", name)
	}
}
