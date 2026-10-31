package types

import (
	"strings"
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

// The other reading of a name is only a collision if a workload could be
// called that. A 50 character workload in b-c reads in c as a 52
// character one, which Workload.Validate refuses.
func TestGatewayNameIgnoresAReadingNoWorkloadCanHave(t *testing.T) {
	t.Parallel()

	cfg := &Config{Gateway: &GatewayConfig{}, Profiles: map[string]Profile{"c": {Name: "c"}, "b-c": {Name: "b-c"}}}
	p := cfg.Profiles["b-c"]
	w := Workload{Name: strings.Repeat("a", 50)}
	require.NoError(t, cfg.ValidateGatewayName(w.ApplyProfile(&p)))
}
