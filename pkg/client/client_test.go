package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_DefaultHTTP(t *testing.T) {
	c := NewClient("http://localhost:16000", "test-key")
	assert.Equal(t, "http://localhost:16000", c.baseURL)
	assert.NotNil(t, c.httpClient)
}

func TestNewClientWithTLS_CACert(t *testing.T) {
	caKey, caCert, caCertPEM := generateTestCA(t)
	serverCertPEM, serverKeyPEM := generateTestServerCert(t, caKey, caCert)

	tlsCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}}
	server.StartTLS()
	defer server.Close()

	c, err := NewClientWithTLS(server.URL, "test-key", writeTempFile(t, caCertPEM, "ca.pem"))
	require.NoError(t, err)

	health, err := c.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)
}

func TestNewClientWithTLS_InvalidCACertPath(t *testing.T) {
	_, err := NewClientWithTLS("https://localhost:16000", "test-key", "/nonexistent/ca.pem")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read CA certificate")
}

func TestNewClientWithTLS_InvalidCACertContent(t *testing.T) {
	badCertFile := writeTempFile(t, []byte("not a certificate"), "bad-ca.pem")
	_, err := NewClientWithTLS("https://localhost:16000", "test-key", badCertFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse CA certificate")
}

func TestNewClientWithTLS_EmptyCACertPath(t *testing.T) {
	c, err := NewClientWithTLS("https://localhost:16000", "test-key", "")
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func generateECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

func generateTestCA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate, []byte) {
	t.Helper()
	key := generateECKey(t)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(certDER)
	require.NoError(t, err)
	return key, cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func generateTestServerCert(t *testing.T, caKey *ecdsa.PrivateKey, caCert *x509.Certificate) ([]byte, []byte) {
	t.Helper()
	key := generateECKey(t)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func writeTempFile(t *testing.T, data []byte, name string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	require.NoError(t, os.WriteFile(path, data, 0644))
	return path
}
