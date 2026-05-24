package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfig_TLSEnabled_MissingCertFile(t *testing.T) {
	config := getDefaultConfig()
	config.Security.TLS.Enabled = true
	config.Security.TLS.CertFile = ""
	config.Security.TLS.KeyFile = "/some/key.pem"

	err := validateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cert_file")
}

func TestValidateConfig_TLSEnabled_MissingKeyFile(t *testing.T) {
	config := getDefaultConfig()
	config.Security.TLS.Enabled = true
	config.Security.TLS.CertFile = "/some/cert.pem"
	config.Security.TLS.KeyFile = ""

	err := validateConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key_file")
}

func TestValidateConfig_TLSDisabled_NoCertRequired(t *testing.T) {
	config := getDefaultConfig()
	config.Security.TLS.Enabled = false

	err := validateConfig(config)
	assert.NoError(t, err)
}

func TestValidateConfig_TLSEnabled_ValidConfig(t *testing.T) {
	config := getDefaultConfig()
	config.Security.TLS.Enabled = true
	config.Security.TLS.CertFile = "/etc/sct-agent/certs/server.crt"
	config.Security.TLS.KeyFile = "/etc/sct-agent/certs/server.key"

	err := validateConfig(config)
	assert.NoError(t, err)
}

func TestLoadConfig_TLSFromYAML(t *testing.T) {
	yamlContent := `
server:
  host: "0.0.0.0"
  port: 16000
security:
  api_keys:
    - "test-key"
  tls:
    enabled: true
    cert_file: "/etc/sct-agent/certs/server.crt"
    key_file: "/etc/sct-agent/certs/server.key"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(yamlContent), 0644))

	config, err := loadConfig(configPath)
	require.NoError(t, err)
	assert.True(t, config.Security.TLS.Enabled)
	assert.Equal(t, "/etc/sct-agent/certs/server.crt", config.Security.TLS.CertFile)
	assert.Equal(t, "/etc/sct-agent/certs/server.key", config.Security.TLS.KeyFile)
}

func TestLoadConfig_NoTLSSection_BackwardCompatible(t *testing.T) {
	yamlContent := `
server:
  host: "0.0.0.0"
  port: 16000
security:
  api_keys:
    - "test-key"
`
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(yamlContent), 0644))

	config, err := loadConfig(configPath)
	require.NoError(t, err)
	assert.False(t, config.Security.TLS.Enabled)
	assert.Empty(t, config.Security.TLS.CertFile)
	assert.Empty(t, config.Security.TLS.KeyFile)
}

func TestDefaultConfig_TLSDisabled(t *testing.T) {
	config := getDefaultConfig()
	assert.False(t, config.Security.TLS.Enabled)
	assert.Empty(t, config.Security.TLS.CertFile)
	assert.Empty(t, config.Security.TLS.KeyFile)
}

func TestLoadTLSConfig_ValidCert(t *testing.T) {
	certFile, keyFile := generateTestCert(t)

	config := getDefaultConfig()
	config.Security.TLS.Enabled = true
	config.Security.TLS.CertFile = certFile
	config.Security.TLS.KeyFile = keyFile

	tlsConfig, err := loadTLSConfig(config)
	require.NoError(t, err)
	assert.NotNil(t, tlsConfig)
	assert.Len(t, tlsConfig.Certificates, 1)
	assert.Equal(t, uint16(tls.VersionTLS12), tlsConfig.MinVersion)
}

func TestLoadTLSConfig_InvalidCertPath(t *testing.T) {
	config := getDefaultConfig()
	config.Security.TLS.Enabled = true
	config.Security.TLS.CertFile = "/nonexistent/cert.pem"
	config.Security.TLS.KeyFile = "/nonexistent/key.pem"

	_, err := loadTLSConfig(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load TLS certificate")
}

func TestLoadTLSConfig_Disabled(t *testing.T) {
	config := getDefaultConfig()
	config.Security.TLS.Enabled = false

	tlsConfig, err := loadTLSConfig(config)
	assert.NoError(t, err)
	assert.Nil(t, tlsConfig)
}

func generateTestCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	require.NoError(t, os.WriteFile(certPath, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0600))

	return certPath, keyPath
}

func TestFormatVersion(t *testing.T) {
	got := formatVersion("v1.2.3", "abc1234", "2026-05-24T12:00:00Z")
	want := "sct-agent v1.2.3 (commit abc1234, built 2026-05-24T12:00:00Z)\n"
	if got != want {
		t.Fatalf("formatVersion mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
