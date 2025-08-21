//go:build !dev

package seed

import (
	"github.com/phishingclub/phishingclub/app"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// InitialInstallAndSeed installs the initial database migrations
func InitialInstallAndSeed(
	db *gorm.DB,
	repositories *app.Repositories,
	logger *zap.SugaredLogger,
	usingSystemd bool,
) error {
	return initialInstallAndSeed(db, repositories, logger, usingSystemd)
}
