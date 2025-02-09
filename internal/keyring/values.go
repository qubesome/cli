package keyring

type SecretName string

const (
	MtlsCA         SecretName = "mtls-ca"
	MtlsClientCert SecretName = "mtls-client-cert"
	MtlsClientKey  SecretName = "mtls-client-key"
)
