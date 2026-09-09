package seccomp

import (
	"encoding/binary"
	"fmt"
	"os"

	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

// MemFD returns the assembled filter in an anonymous file, positioned at
// the start, ready to be passed to bwrap as --seccomp.
//
// A memfd is used rather than a temp file so the filter never touches the
// filesystem, where another process could read or replace it between
// writing and exec.
//
// The fd is created with MFD_CLOEXEC so it is not inherited by a process
// this package's caller execs for some other purpose. Passing it to bwrap
// through exec.Cmd.ExtraFiles still works: os/exec dups each ExtraFiles
// entry onto a low fd in the child before the exec, and that dup is made
// without the close-on-exec flag, so the duplicate the child inherits is
// open across the exec regardless of the flag on the original descriptor.
func MemFD() (*os.File, error) {
	p, err := Load()
	if err != nil {
		return nil, err
	}

	insns, err := p.Program()
	if err != nil {
		return nil, err
	}

	raw, err := bpf.Assemble(insns)
	if err != nil {
		return nil, fmt.Errorf("failed to assemble seccomp filter: %w", err)
	}

	fd, err := unix.MemfdCreate("qubesome-seccomp", unix.MFD_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("failed to create memfd for seccomp filter: %w", err)
	}
	f := os.NewFile(uintptr(fd), "qubesome-seccomp")

	// struct sock_filter holds four fields in order: a __u16 code, a __u8
	// jt, a __u8 jf, and a __u32 k. That is 8 bytes with natural alignment,
	// in the host's native byte order. Every architecture qubesome builds
	// for (amd64, arm64) is little endian, so encoding as little endian
	// here matches native order on all of them.
	b := make([]byte, 0, len(raw)*8)
	for _, i := range raw {
		b = binary.LittleEndian.AppendUint16(b, i.Op)
		b = append(b, i.Jt, i.Jf)
		b = binary.LittleEndian.AppendUint32(b, i.K)
	}

	if _, err := f.Write(b); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to write seccomp filter: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to rewind seccomp filter: %w", err)
	}

	return f, nil
}
