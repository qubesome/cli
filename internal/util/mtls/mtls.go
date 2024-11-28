package mtls

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

const (
	validFor = 7 * 24 * time.Hour // 7 days
	// ProfileServerName sets the server name for the qubesome profile.
	ProfileServerName = "qubesome-profile"
	// HostServerName sets the server name for the qubesome host.
	HostServerName = "qubesome-host"
)

type Credentials struct {
	ServerCert   tls.Certificate
	CA           []byte
	ClientPEM    []byte
	ClientKeyPEM []byte
}

func NewCredentials() (*Credentials, error) {
	caCert, caKey, caBytes, err := generateCA()
	if err != nil {
		return nil, err
	}

	serverCertBytes, serverKey, err := generateCert(caCert, caKey, true)
	if err != nil {
		return nil, err
	}
	serverCertPEM, serverKeyPEM, err := pemEncode(serverCertBytes, serverKey)
	if err != nil {
		return nil, err
	}

	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		return nil, err
	}

	clientCertBytes, clientKey, err := generateCert(caCert, caKey, false)
	if err != nil {
		return nil, err
	}
	clientCertPEM, clientKeyPEM, err := pemEncode(clientCertBytes, clientKey)
	if err != nil {
		return nil, err
	}

	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caBytes})
	return &Credentials{
		ServerCert:   serverCert,
		CA:           ca,
		ClientPEM:    clientCertPEM,
		ClientKeyPEM: clientKeyPEM,
	}, nil
}

// generateCA generates an in-memory CA certificate and private key.
func generateCA() (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   "qubesome inception CA",
			Organization: []string{"qubesome"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(validFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		SignatureAlgorithm:    x509.ECDSAWithSHA256,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	certPEM := new(bytes.Buffer)
	err = pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to PEM encode certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	return cert, priv, certBytes, nil
}

// generateCert generates a certificate signed by caCert.
func generateCert(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, isServer bool) ([]byte, *ecdsa.PrivateKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			Organization: []string{"qubesome"},
		},
		NotBefore:          time.Now(),
		NotAfter:           time.Now().Add(validFor),
		KeyUsage:           x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:        []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}

	if isServer {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.DNSNames = []string{HostServerName}
	} else {
		template.DNSNames = []string{ProfileServerName}
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, caCert, &priv.PublicKey, caKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	return certBytes, priv, nil
}

// pemEncode encodes the certificate and private key to PEM format.
func pemEncode(certBytes []byte, priv *ecdsa.PrivateKey) ([]byte, []byte, error) {
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	return certPEM, keyPEM, nil
}
