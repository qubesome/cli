package gateway

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarise(t *testing.T) {
	t.Parallel()

	path := writeLog(t,
		`level=INFO msg="starting gateway"`,
		`level=INFO msg="proxy decision" workload=cli-llm-work host=api.anthropic.com action=splice`,
		`level=INFO msg="proxy decision" workload=chrome-work host=ads.example.com action=deny reason="policy denied host"`,
		`level=INFO msg="proxy decision" workload=chrome-work host=eu.ads.example.com action=deny reason="policy denied host"`,
		`level=INFO msg="proxy decision" workload=chrome-work host=ads.example.com action=deny reason="policy denied host"`,
		`level=WARN msg="splice dial failed" workload=chrome-work host=slow.example`,
		`level=ERROR msg="original destination lookup failed" plane=proxy`,
		`level=INFO msg="proxy decision" workload=cli-llm-work host=api.github.com action=inject`,
	)

	got, err := Summarise(path)
	require.NoError(t, err)

	assert.Equal(t, 5, got.Decisions, "every proxy decision counts, whatever its verdict")
	assert.Equal(t, 3, got.Denied)
	assert.Equal(t, []string{"ads.example.com", "eu.ads.example.com"}, got.DeniedHosts,
		"a host denied twice is named once")
	assert.Equal(t, 1, got.Errors, "a warning is not an error")
	assert.Equal(t, "original destination lookup failed", got.LastError)
}

func TestSummariseAQuietLog(t *testing.T) {
	t.Parallel()

	got, err := Summarise(writeLog(t, `level=INFO msg="starting gateway"`))
	require.NoError(t, err)

	assert.Zero(t, got.Decisions)
	assert.Zero(t, got.Denied)
	assert.Empty(t, got.DeniedHosts)
	assert.Zero(t, got.Errors)
}

func TestSummariseWithoutALog(t *testing.T) {
	t.Parallel()

	_, err := Summarise(filepath.Join(t.TempDir(), "absent.log"))
	require.Error(t, err)
	assert.ErrorIs(t, err, errNoLog)
}

// A gateway that denies a great many hosts must not turn a diagnosis into
// a list of them.
func TestSummariseCapsTheHostsItNames(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, maxDeniedHosts*2)
	for i := range maxDeniedHosts * 2 {
		lines = append(lines,
			`level=INFO msg="proxy decision" workload=w-p action=deny host=h`+
				string(rune('a'+i))+`.example`)
	}

	got, err := Summarise(writeLog(t, lines...))
	require.NoError(t, err)

	assert.Equal(t, maxDeniedHosts*2, got.Denied, "the count is of every denial")
	assert.Len(t, got.DeniedHosts, maxDeniedHosts, "the names are capped")
}
