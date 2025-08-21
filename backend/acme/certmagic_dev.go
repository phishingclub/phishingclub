//go:build dev

package acme

import (
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"log"

	"github.com/caddyserver/certmagic"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/errs"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const DEV_ACME_URL = "https://pebble:14000/dir"

//go:embed pebble.minica.pem
var acmeRootCertPemBlock []byte

func loadDevelopmentPebbleCertificate() (*x509.Certificate, error) {
	certDERBlock, _ := pem.Decode(acmeRootCertPemBlock)
	if certDERBlock == nil {
		log.Fatal("Failed to parse the certificate PEM.")
	}
	acmeRootCert, err := x509.ParseCertificate(certDERBlock.Bytes)
	if err != nil {
		log.Fatal(err)
	}
	return acmeRootCert, nil
}

// SetupCertMagic creates a certmagic config for development
// and checks which domains are allowed from the db before getting a certificate
func SetupCertMagic(
	certStoragePath string,
	conf *config.Config,
	db *gorm.DB,
	logger *zap.SugaredLogger,
) (*certmagic.Config, *certmagic.Cache, error) {
	cert, err := loadDevelopmentPebbleCertificate()
	if err != nil {
		return nil, nil, errs.Wrap(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	certmagic.DefaultACME = certmagic.ACMEIssuer{
		CA:           DEV_ACME_URL,
		TestCA:       DEV_ACME_URL,
		Agreed:       true,
		TrustedRoots: pool,
	}
	return setupCertMagic(certStoragePath, conf, db, logger)
}
