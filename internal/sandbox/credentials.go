package sandbox

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

type RuntimeCredentials struct {
	CAPEM         []byte
	ServerCertPEM []byte
	ServerKeyPEM  []byte
	ClientCertPEM []byte
	ClientKeyPEM  []byte
	Token         string
}

func GenerateRuntimeCredentials(serviceNames []string) (RuntimeCredentials, error) {
	now := time.Now().UTC()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return RuntimeCredentials{}, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber: randomSerial(), Subject: pkix.Name{CommonName: "Haro Sandbox Runtime CA"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(5, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return RuntimeCredentials{}, err
	}
	serverCert, serverKey, err := issueCertificate(caTemplate, caKey, now, "haro-sandboxd", serviceNames, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	if err != nil {
		return RuntimeCredentials{}, err
	}
	clientCert, clientKey, err := issueCertificate(caTemplate, caKey, now, "haro-bot", nil, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	if err != nil {
		return RuntimeCredentials{}, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return RuntimeCredentials{}, err
	}
	return RuntimeCredentials{
		CAPEM:         pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		ServerCertPEM: serverCert, ServerKeyPEM: serverKey, ClientCertPEM: clientCert, ClientKeyPEM: clientKey,
		Token: fmt.Sprintf("%x", tokenBytes),
	}, nil
}

func issueCertificate(ca *x509.Certificate, caKey *rsa.PrivateKey, now time.Time, commonName string, dnsNames []string, usage []x509.ExtKeyUsage) ([]byte, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: randomSerial(), Subject: pkix.Name{CommonName: commonName}, DNSNames: dnsNames,
		NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: usage,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), nil
}

func randomSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return value
}
