package bwrap

import (
	"testing"

	"github.com/qubesome/cli/internal/images"
	"github.com/qubesome/cli/internal/types"
	"github.com/stretchr/testify/assert"
)

// A workload that has a gateway is told where to ask it for a tunnel. One
// without a gateway has nothing to be told, and an empty variable would
// read as an endpoint of nothing at all.
func TestWorkloadEnvNamesTheGatewayProxy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		proxy string
		want  bool
	}{
		{"attached to a gateway", "10.111.0.1:3128", true},
		{"no gateway", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := workloadEnv(input{
				Workload: types.EffectiveWorkload{
					Name:     "w-p",
					Profile:  &types.Profile{Name: "p"},
					Workload: types.Workload{},
				},
				Bundle:       images.Bundle{},
				GatewayProxy: tc.proxy,
			})

			if tc.want {
				assert.Contains(t, env, "QUBESOME_GATEWAY_PROXY="+tc.proxy)

				return
			}
			for _, e := range env {
				assert.NotContains(t, e, "QUBESOME_GATEWAY_PROXY")
			}
		})
	}
}
