//go:build !dev

package acme

import (
	_ "embed"

	"github.com/caddyserver/certmagic"
	"github.com/phishingclub/phishingclub/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SetupCertMagic creates a certmagic config for development
// and checks which domains are allowed from the db before getting a certificate
func SetupCertMagic(
	certStoragePath string,
	conf *config.Config,
	db *gorm.DB,
	logger *zap.SugaredLogger,
) (*certmagic.Config, *certmagic.Cache, error) {
	return setupCertMagic(certStoragePath, conf, db, logger)
}
