package seccomp

import (
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemFD(t *testing.T) {
	t.Parallel()

	f, err := MemFD()
	require.NoError(t, err)
	defer f.Close()

	b, err := io.ReadAll(f)
	require.NoError(t, err)

	require.NotEmpty(t, b)
	assert.Zero(t, len(b)%8, "a BPF program is a whole number of 8 byte instructions")

	// The first instruction loads seccomp_data.arch, which is a 4 byte
	// absolute load: code BPF_LD|BPF_W|BPF_ABS (0x20) at offset 4.
	assert.Equal(t, uint16(0x20), binary.LittleEndian.Uint16(b[0:2]))
	assert.Equal(t, uint32(offArch), binary.LittleEndian.Uint32(b[4:8]))
}

func TestMemFDIsReadableFromStart(t *testing.T) {
	t.Parallel()

	f, err := MemFD()
	require.NoError(t, err)
	defer f.Close()

	off, err := f.Seek(0, io.SeekCurrent)
	require.NoError(t, err)
	assert.Zero(t, off, "bwrap reads the fd from wherever it is left")
}
