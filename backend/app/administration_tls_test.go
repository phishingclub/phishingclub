package app

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/phishingclub/phishingclub/acme"
	"github.com/phishingclub/phishingclub/config"
	"go.uber.org/zap"
)

// seedCert writes a self signed certificate covering the given hostnames to the
// paths and returns the parsed certificate, standing in for a certificate an
// operator supplied.
func seedCert(t *testing.T, certPath, keyPath string, hostnames []string) *x509.Certificate {
	t.Helper()
	info := acme.NewInformationWithDefault()
	if len(hostnames) > 0 {
		info.CommonName = hostnames[0]
	}
	if err := acme.CreateSelfSignedCert(zap.NewNop().Sugar(), info, hostnames, certPath, keyPath); err != nil {
		t.Fatalf("seed certificate: %v", err)
	}
	return parseCert(t, certPath)
}

func parseCert(t *testing.T, certPath string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read certificate: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("decode certificate pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

// An operator runs own managed TLS (tls_auto false) with their own certificate,
// here a wildcard, and sets tls_host to the concrete admin host they use. The
// concrete host is not literally a name in the wildcard certificate. Startup
// must not overwrite the operator certificate.
func TestHandleTLSCertificate_KeepsProvidedCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "public.pem")
	keyPath := filepath.Join(dir, "private.pem")

	seed := seedCert(t, certPath, keyPath, []string{"*.example.com"})

	conf := config.NewDevDefaultConfig()
	conf.SetTLSAuto(false)
	conf.SetTLSHost("admin.example.com")
	conf.SetTLSCertPath(certPath)
	conf.SetTLSKeyPath(keyPath)

	a := &administrationServer{logger: zap.NewNop().Sugar()}
	if err := a.handleTLSCertificate(conf); err != nil {
		t.Fatalf("handleTLSCertificate: %v", err)
	}

	after := parseCert(t, certPath)
	if after.SerialNumber.Cmp(seed.SerialNumber) != 0 {
		t.Fatalf("operator certificate was overwritten: serial changed %s -> %s", seed.SerialNumber, after.SerialNumber)
	}
}

// With no certificate present the admin interface must still come up, so a self
// signed certificate is created on first run.
func TestHandleTLSCertificate_CreatesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "public.pem")
	keyPath := filepath.Join(dir, "private.pem")

	conf := config.NewDevDefaultConfig()
	conf.SetTLSAuto(false)
	conf.SetTLSHost("admin.example.com")
	conf.SetTLSCertPath(certPath)
	conf.SetTLSKeyPath(keyPath)

	a := &administrationServer{logger: zap.NewNop().Sugar()}
	if err := a.handleTLSCertificate(conf); err != nil {
		t.Fatalf("handleTLSCertificate: %v", err)
	}

	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("expected a certificate to be created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("expected a key to be created: %v", err)
	}
}
