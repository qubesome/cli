package seccomp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "SCMP_ACT_ERRNO", p.DefaultAction)
	require.NotNil(t, p.DefaultErrnoRet)
	assert.Equal(t, uint32(38), *p.DefaultErrnoRet)
	assert.NotEmpty(t, p.Syscalls)
}

// Every rule group carrying argument conditions in the vendored profile is
// gated on excludes.caps. The sandbox drops all capabilities, so those
// groups always apply and the emitter needs no capability model. This
// assumption is load-bearing, so a profile update that breaks it must fail
// here rather than silently widen the filter.
func TestConditionalRulesAreCapabilityExcludedOnly(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	for _, r := range p.Syscalls {
		if len(r.Args) == 0 {
			continue
		}
		assert.Empty(t, r.Includes.Caps, "rule %v has includes.caps", r.Names)
		assert.Empty(t, r.Includes.Arches, "rule %v has includes.arches", r.Names)
		assert.Empty(t, r.Excludes.Arches, "rule %v has excludes.arches", r.Names)
	}
}

func TestOnlyKnownComparisonOperators(t *testing.T) {
	t.Parallel()

	p, err := Load()
	require.NoError(t, err)

	for _, r := range p.Syscalls {
		for _, a := range r.Args {
			assert.Contains(t, []string{"SCMP_CMP_EQ", "SCMP_CMP_NE"}, a.Op,
				"rule %v uses operator %s", r.Names, a.Op)
		}
	}
}
