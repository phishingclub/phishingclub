package acme

import (
	_ "embed"

	"github.com/caddyserver/certmagic"
	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/database"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
)

// maintenanceCore wraps the original core to filter maintenance messages
type maintenanceCore struct {
	zapcore.Core
	originalCore zapcore.Core
}

func (c *maintenanceCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if ent.Message == "started background certificate maintenance" {
		c.Core = c.originalCore
		return nil
	}
	return c.Core.Check(ent, ce)
}

func (c *maintenanceCore) With(fields []zapcore.Field) zapcore.Core {
	return &maintenanceCore{
		Core:         c.Core.With(fields),
		originalCore: c.originalCore,
	}
}

func setupCertMagic(
	certStoragePath string,
	conf *config.Config,
	db *gorm.DB,
	logger *zap.SugaredLogger,
) (*certmagic.Config, *certmagic.Cache, error) {
	l := logger.Desugar()
	usedLogger := l.Core()
	if l.Level() != zap.DebugLevel {
		usedLogger = &maintenanceCore{
			Core:         l.Core(),
			originalCore: usedLogger,
		}
	}
	filteredLogger := zap.New(usedLogger)

	// Create main config first
	certmagic.DefaultACME.Logger = l
	certmagic.DefaultACME.Email = conf.ACMEEmail()
	mainConfig := certmagic.NewDefault()
	mainConfig.Logger = l
	mainConfig.Storage = &certmagic.FileStorage{Path: certStoragePath}
	mainConfig.OnDemand = &certmagic.OnDemandConfig{
		DecisionFunc: func(name string) error {
			// check if admin server with auto TLS
			if conf.TLSAuto() && conf.TLSHost() == name {
				return nil
			}
			// check phishing host with managed TLS
			res := db.
				Select("id").
				Where("name = ?", name).
				Where("managed_tls_certs IS true").
				First(&database.Domain{})

			if res.RowsAffected > 0 {
				return nil
			}
			return errors.Errorf("not allowing TLS on-demand request for '%s'", name)
		},
	}
	// create cache with config getter
	var finalConfig *certmagic.Config
	defaultCache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(cert certmagic.Certificate) (*certmagic.Config, error) {
			return finalConfig, nil
		},
		Logger: filteredLogger,
	})
	// create final config that uses the cache
	finalConfig = certmagic.New(defaultCache, *mainConfig)

	return finalConfig, defaultCache, nil
}
