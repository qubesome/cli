package keyring_test

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/qubesome/cli/internal/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSet(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		data    map[string]string
		key     keyring.SecretName
		want    string
		wantErr bool
	}{
		{
			name:    "existing key",
			profile: "foo",
			data: map[string]string{
				"qubesome:foo:mtls-ca": "bar",
			},
			key:  keyring.MtlsCA,
			want: "bar",
		},
		{
			name:    "other profiles and keys",
			profile: "bar",
			data: map[string]string{
				"qubesome:bar:mtls-client-key": "foo",
			},
			key:  keyring.MtlsClientKey,
			want: "foo",
		},
		{
			name:    "key not found",
			profile: "foo",
			data: map[string]string{
				"qubesome:bar:mtls-ca": "bar",
			},
			key:     keyring.MtlsCA,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ks := keyring.New(tc.profile, newBackend(tc.data))

			got, err := ks.Get(tc.key)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}

			newValue := randValue()
			err = ks.Set(tc.key, newValue)
			require.NoError(t, err)

			val, err := ks.Get(tc.key)
			require.NoError(t, err)
			assert.Equal(t, newValue, val)
		})
	}
}

func randValue() string {
	d := make([]byte, 16)
	if _, err := rand.Read(d); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(d)
}

func newBackend(data map[string]string) *mockBackend {
	return &mockBackend{data}
}

type mockBackend struct {
	data map[string]string
}

func (m *mockBackend) Get(service, user string) (string, error) {
	if val, ok := m.data[service]; ok {
		return val, nil
	}
	return "", fmt.Errorf("not found")
}

func (m *mockBackend) Set(service, user, value string) error {
	m.data[service] = value
	return nil
}

func (m *mockBackend) Delete(service, user string) error {
	delete(m.data, service)
	return nil
}
