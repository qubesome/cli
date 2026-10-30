package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qubesome/cli/pkg/control"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const keyringPolicy = `workloads:
  code-work:
    inject:
      - host: api.github.com
        header: Authorization
        valueFrom:
          keyring:
            service: qubesome/work/github
            account: token
  code-personal:
    egress:
      allowed: [api.github.com]
`

func TestKeyringReadsOnlyExplicitInjectionReferences(t *testing.T) {
	t.Parallel()
	calls := 0
	values, err := keyringValues([]byte(keyringPolicy), func(service, account string) (string, error) {
		calls++
		require.Equal(t, "qubesome/work/github", service)
		require.Equal(t, "token", account)
		return "host-token", nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, map[string]string{control.KeyringID("qubesome/work/github", "token"): "host-token"}, values)
}

func TestKeyringFailsClosedWithoutExposingBackendErrors(t *testing.T) {
	t.Parallel()
	_, err := keyringValues([]byte(keyringPolicy), func(string, string) (string, error) {
		return "", errors.New("backend leaked SUPERSECRET")
	})
	require.ErrorContains(t, err, "cannot read host keyring")
	require.NotContains(t, err.Error(), "SUPERSECRET")
	for _, value := range []string{"", "token\r\nInjected: yes", "token\x00"} {
		_, err := keyringValues([]byte(keyringPolicy), func(string, string) (string, error) { return value, nil })
		require.Error(t, err)
	}
}

func TestKeyringRejectsMixedSourcesBeforeReadingStore(t *testing.T) {
	t.Parallel()
	data := strings.Replace(keyringPolicy, "          keyring:", "          file: /run/secrets/token\n          keyring:", 1)
	_, err := keyringValues([]byte(data), func(string, string) (string, error) {
		t.Fatal("must not read store for an invalid reference")
		return "", nil
	})
	require.ErrorContains(t, err, "cannot be combined")
}

func TestPolicyStagingContainsReferencesOnlyAndSupportsReplacement(t *testing.T) {
	t.Parallel()
	g := Gateway{Dir: t.TempDir()}
	require.NoError(t, g.stagePolicy([]byte(keyringPolicy)))
	path := filepath.Join(g.policyDir(), "gateway.yml")
	old, err := os.Open(path)
	require.NoError(t, err)
	defer old.Close()
	replacement := strings.ReplaceAll(keyringPolicy, "api.github.com", "api.example.com")
	require.NoError(t, g.stagePolicy([]byte(replacement)))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, replacement, string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestKeyringClientRequiresExplicitGatewaySupport(t *testing.T) {
	g := newGateway(closedChan())
	c := startGateway(t, g)
	err := c.ReloadKeyring(map[string]string{"id": "secret"}, make([]byte, 32))
	require.ErrorContains(t, err, "does not support host-keyring")
}

const plainPolicy = `workloads:
  code-personal:
    egress:
      allowed: [api.github.com]
`

func gatewayWithPolicy(t *testing.T, policy string) Gateway {
	t.Helper()

	dir := t.TempDir()
	g := Gateway{Dir: dir}
	source := filepath.Join(dir, "gateway.yml")
	require.NoError(t, os.WriteFile(source, []byte(policy), 0o600))
	require.NoError(t, os.WriteFile(g.policySourcePath(), []byte(source), 0o600))
	require.NoError(t, g.stagePolicy([]byte(policy)))

	return g
}

// A gateway image from before Reload was served never re-read its policy,
// and a launch against one carried on with a warning. A policy that asks
// for nothing from the host keyring must keep it that way.
func TestReloadToleratesAGatewayThatCannotReload(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	gw.noReload = true
	c := startGateway(t, gw)

	require.NoError(t, gatewayWithPolicy(t, plainPolicy).reloadWith(c, false, ""))
}

// A gateway this launch started has just read its policy, so with no host
// keyring values to deliver there is nothing to ask of it.
func TestReloadSkipsAGatewayJustStarted(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	require.NoError(t, gatewayWithPolicy(t, plainPolicy).reloadWith(c, true, ""))
	assert.Equal(t, 0, gw.reloaded())
}

func TestReloadReReadsARunningGatewaysPolicy(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	require.NoError(t, gatewayWithPolicy(t, plainPolicy).reloadWith(c, false, ""))
	assert.Equal(t, 1, gw.reloaded())
}

// The gateway reads the staged copy as it starts, and an image pull can sit
// between that and the launch staging the policy again. An edit made then
// is one the gateway has not read.
func TestReloadReReadsAPolicyEditedWhileTheGatewayStarted(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	edited := strings.ReplaceAll(plainPolicy, "api.github.com", "api.example.com")
	g := gatewayWithPolicy(t, edited)
	require.NoError(t, g.stagePolicy([]byte(plainPolicy)))

	require.NoError(t, g.reloadWith(c, true, ""))
	assert.Equal(t, 1, gw.reloaded())
}

// A gateway started by a qubesome from before the source was recorded
// cannot be handed keyring values. A launch whose policy names some is
// refused rather than run without the credentials it was given.
func TestReloadRefusesAGatewayWithNoSourceForAKeyringPolicy(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	dir := t.TempDir()
	launch := filepath.Join(dir, "gateway.yml")
	require.NoError(t, os.WriteFile(launch, []byte(keyringPolicy), 0o600))

	g := Gateway{Dir: dir}
	require.ErrorIs(t, g.reloadWith(c, false, launch), errNoPolicySource)
	require.ErrorIs(t, g.reloadWith(c, false, ""), errNoPolicySource)
	assert.Equal(t, 0, gw.reloaded())
}

func TestReloadKeepsAGatewayWithNoSourceWorkingForAPlainPolicy(t *testing.T) {
	gw := newGateway(closedChan(), testWorkload)
	c := startGateway(t, gw)

	dir := t.TempDir()
	launch := filepath.Join(dir, "gateway.yml")
	require.NoError(t, os.WriteFile(launch, []byte(plainPolicy), 0o600))

	require.NoError(t, Gateway{Dir: dir}.reloadWith(c, false, launch))
	assert.Equal(t, 1, gw.reloaded())
}
