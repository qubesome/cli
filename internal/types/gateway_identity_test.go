package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayRejectsAmbiguousProfileWorkloadNames(t *testing.T) {
	t.Parallel()
	cfg := &Config{Gateway: &GatewayConfig{}, Profiles: map[string]Profile{"c": {Name: "c"}, "b-c": {Name: "b-c"}}}
	p := cfg.Profiles["c"]
	ew := (Workload{Name: "a-b"}).ApplyProfile(&p)
	require.Equal(t, "a-b-c", ew.Name)
	require.ErrorContains(t, cfg.ValidateGatewayName(ew), "ambiguous gateway workload name")
	p = cfg.Profiles["b-c"]
	ew = (Workload{Name: "a"}).ApplyProfile(&p)
	require.ErrorContains(t, cfg.ValidateGatewayName(ew), "ambiguous gateway workload name")

	p = cfg.Profiles["c"]
	require.NoError(t, cfg.ValidateGatewayName((Workload{Name: "x"}).ApplyProfile(&p)))
}
