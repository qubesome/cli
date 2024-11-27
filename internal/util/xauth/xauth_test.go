package xauth

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthPair(t *testing.T) {
	tests := []struct {
		name    string
		display uint8
		parent  string
		server  string
		client  string
		err     string
	}{
		{
			name:    "display 0",
			parent:  "01000005712d706f6400013000127749542d4d411149432d411f4f4b66452d01001040cc3e730e6f7a534a3977321b14e0a7",
			display: 0,
			server:  "01000005712d706f6400013000127749542d4d411149432d411f4f4b66452d010010ffffffffffffffffffffffffffffffff",
			client:  "ffff0005712d706f6400013000127749542d4d411149432d411f4f4b66452d010010ffffffffffffffffffffffffffffffff",
		},
		{
			name:    "display 5",
			parent:  "01000005712d706f6400013000127749542d4d411149432d411f4f4b66452d01001040cc3e730e6f7a534a3977321b14e0a7",
			display: 5,
			server:  "01000005712d706f6400013500127749542d4d411149432d411f4f4b66452d010010ffffffffffffffffffffffffffffffff",
			client:  "ffff0005712d706f6400013500127749542d4d411149432d411f4f4b66452d010010ffffffffffffffffffffffffffffffff",
		},
	}

	old := cookieFunc
	cookieFunc = func() ([]byte, error) {
		return hex.DecodeString("ffffffffffffffffffffffffffffffff")
	}
	defer func() { cookieFunc = old }()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := bytes.NewBuffer(nil)
			client := bytes.NewBuffer(nil)

			parent, err := hex.DecodeString(tc.parent)
			require.NoError(t, err)

			got := AuthPair(tc.display, bytes.NewReader(parent), server, client)
			if tc.err == "" {
				require.NoError(t, got)

				wantServer, err := hex.DecodeString(tc.server)
				require.NoError(t, err)

				wantClient, err := hex.DecodeString(tc.client)
				require.NoError(t, err)

				assert.Equal(t, wantServer, server.Bytes())
				assert.Equal(t, wantClient, client.Bytes())
			} else {
				require.ErrorContains(t, got, tc.err)

				assert.Empty(t, server)
				assert.Empty(t, client)
			}
		})
	}
}

func TestToNumeric(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want string
		err  string
	}{
		{
			name: "empty",
			hex:  "",
			want: "",
		},
		{
			name: "full",
			hex:  "01000005701d503f6400013000122249542d4d411149432d411f4f4b22152d01001040cc3e530e6f7a534a3977321b14e0a7",
			want: "0100 0005 701d503f64 0001 30 0012 2249542d4d411149432d411f4f4b22152d01 0010 40cc3e530e6f7a534a3977321b14e0a7",
		},
		{
			name: "short",
			hex:  "01000005701d503f6400013000122249542d4d411149432d411f4f4b22152d010010",
			want: "0100 0005 701d503f64 0001 30 0012 2249542d4d411149432d411f4f4b22152d01 0010 ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in, err := hex.DecodeString(tc.hex)
			require.NoError(t, err)

			got := ToNumeric(in)

			assert.Equal(t, tc.want, got)
		})
	}
}

func FuzzToNumberic(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte) {
		ToNumeric(input)
	})
}

func FuzzAuthPair(f *testing.F) {
	f.Fuzz(func(t *testing.T, display uint8, parent []byte) {
		_ = AuthPair(display, bytes.NewReader(parent),
			&bytes.Buffer{}, &bytes.Buffer{})
	})
}
