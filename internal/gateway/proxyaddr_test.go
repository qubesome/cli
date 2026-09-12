package gateway

import (
	"testing"

	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A workload reaches the proxy at the gateway's own address, which is the
// first host address of the subnet and is already its default route and
// its resolver. What it cannot work out for itself is the port, so the
// whole endpoint is what it is told.
func TestAttachProxyAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		subnet string
		want   string
	}{
		{"the usual subnet", "10.111.0.0/24", "10.111.0.1:3128"},
		{"another range", "192.168.44.0/24", "192.168.44.1:3128"},
		{"a small one", "10.9.9.8/30", "10.9.9.9:3128"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			a := &Attach{config: types.GatewayConfig{Subnet: tc.subnet}}

			got, err := a.ProxyAddr()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAttachProxyAddrWithoutASubnet(t *testing.T) {
	t.Parallel()

	a := &Attach{config: types.GatewayConfig{Subnet: "not a subnet"}}

	_, err := a.ProxyAddr()
	require.Error(t, err)
}
