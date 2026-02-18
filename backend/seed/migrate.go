package seed

import (
	"crypto/rand"
	"strings"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/app"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// InitialInstallAndSeed installs the initial database migrations
func initialInstallAndSeed(
	db *gorm.DB,
	repositories *app.Repositories,
	logger *zap.SugaredLogger,
	usingSystemd bool,
) error {
	tables := []any{
		&database.Asset{},
		&database.Option{},
		&database.Company{},
		&database.APISender{},
		&database.APISenderHeader{},
		&database.Role{},
		&database.User{},
		&database.Session{},
		&database.Recipient{},
		&database.RecipientGroup{},
		&database.RecipientGroupRecipient{},
		&database.Domain{},
		&database.Page{},
		&database.Proxy{},
		&database.SMTPHeader{},
		&database.SMTPConfiguration{},
		&database.Email{},
		&database.CampaignTemplate{},
		&database.Campaign{},
		&database.CampaignRecipientGroup{},
		&database.CampaignRecipient{},
		&database.Event{},
		&database.CampaignEvent{},
		&database.Attachment{},
		&database.EmailAttachment{},
		&database.AllowDeny{},
		&database.CampaignAllowDeny{},
		&database.Webhook{},
		&database.CampaignWebhook{},
		&database.Identifier{},
		&database.CampaignStats{},
		&database.OAuthProvider{},
		&database.OAuthState{},
	}

	// disable foreign key constraints temporarily for sqlite to allow table recreation
	logger.Debug("disabling foreign key constraints for migration")
	err := db.Exec("PRAGMA foreign_keys = OFF").Error
	if err != nil {
		return errs.Wrap(errors.Errorf("failed to disable foreign keys: %w", err))
	}

	// create tables
	logger.Debug("migrating tables")
	err = db.AutoMigrate(
		tables...,
	)
	if err != nil {
		// re-enable foreign keys before returning error
		db.Exec("PRAGMA foreign_keys = ON")
		return errs.Wrap(
			errors.Errorf("failed to migrate database: %w", err),
		)
	}

	// re-enable foreign key constraints
	logger.Debug("re-enabling foreign key constraints after migration")
	err = db.Exec("PRAGMA foreign_keys = ON").Error
	if err != nil {
		return errs.Wrap(errors.Errorf("failed to re-enable foreign keys: %w", err))
	}
	for _, table := range tables {
		t, ok := table.(database.Migrater)
		if !ok {
			// logger.Debugw("table has no extra migration", "table", table)
			continue
		}
		// logger.Debugw("running extra migration for table", "table", table)
		err := t.Migrate(db)
		if err != nil {
			return errs.Wrap(
				errors.Errorf("failed to run extra migration for table %T: %w", table, err),
			)
		}
	}
	// seed settings levels default values
	err = SeedSettings(db, usingSystemd)
	if err != nil {
		return errs.Wrap(
			errors.Errorf("failed to seed log levels: %w", err),
		)
	}
	// seed user roles
	err = SeedRoles(repositories.Role)
	if err != nil {
		return errs.Wrap(
			errors.Errorf("failed to seed roles: %w", err),
		)
	}
	// seed events
	err = SeedEvents(db)
	if err != nil {
		return errs.Wrap(
			errors.Errorf("failed to seed events: %w", err),
		)
	}
	// seed identifiers
	err = SeedIdentifiers(db, repositories.Identifier)
	if err != nil {
		return errs.Wrap(
			errors.Errorf("failed to seed identifiers: %w", err),
		)
	}
	return nil
}

func SeedSettings(
	db *gorm.DB,
	usingSystemd bool,
) error {
	// seed log levels
	if build.Flags.Production {
		err := seedLogLevels(db, "info", "silent")
		if err != nil {
			return err
		}
	} else {
		err := seedLogLevels(db, "info", "info")
		if err != nil {
			return err
		}
	}
	{
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyDBLogLevel).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyDBLogLevel,
				Value: "info",
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	// seed max file size
	{
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyMaxFileUploadSizeMB).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyMaxFileUploadSizeMB,
				Value: data.OptionValueKeyMaxFileUploadSizeMBDefault,
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	{
		// seed repeat offender threshold
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyRepeatOffenderMonths).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyRepeatOffenderMonths,
				Value: "12", // Default to 12 months
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	{
		// seed sso option
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyAdminSSOLogin).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if c == 0 {
			v, err := model.NewSSOOptionDefault().ToJSON()
			if err != nil {
				return errs.Wrap(err)
			}
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyAdminSSOLogin,
				Value: string(v),
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	{
		// seed obfuscation template option
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyObfuscationTemplate).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyObfuscationTemplate,
				Value: data.OptionValueObfuscationTemplateDefault,
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	{
		// seed display mode option
		// default to blackbox if option doesn't exist
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyDisplayMode).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyDisplayMode,
				Value: data.OptionValueDisplayModeBlackbox,
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	{
		// seed using systemd option
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyUsingSystemd).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		isUsingSystemdStr := data.OptionValueUsingSystemdYes
		if !usingSystemd {
			isUsingSystemdStr = data.OptionValueUsingSystemdNo
		}
		if c == 0 {
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyUsingSystemd,
				Value: isUsingSystemdStr,
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	{
		// seed proxy cookie name
		id := uuid.New()
		var c int64
		res := db.
			Model(&database.Option{}).
			Where("key = ?", data.OptionKeyProxyCookieName).
			Count(&c)

		if res.Error != nil {
			return errs.Wrap(res.Error)
		}
		if c == 0 {
			// generate random 8-character cookie name
			b := make([]byte, 8)
			_, err := rand.Read(b)
			if err != nil {
				return errs.Wrap(err)
			}
			charset := "abcdefghijklmnopqrstuvwxyz"
			cookieName := ""
			for i := range b {
				cookieName += string(charset[int(b[i])%len(charset)])
			}
			res = db.Create(&database.Option{
				ID:    &id,
				Key:   data.OptionKeyProxyCookieName,
				Value: cookieName,
			})
			if res.Error != nil {
				return errs.Wrap(res.Error)
			}
		}
	}
	return nil
}

// Migration migrates db
func migrate(db *gorm.DB) error {
	// migration for attachments.embedded_content
	// first add column as nullable
	if err := db.Exec(`ALTER TABLE attachments ADD COLUMN embedded_content BOOLEAN`).Error; err != nil {
		// column might already exist, ignore error
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "duplicate") && !strings.Contains(errMsg, "already exists") {
			return errs.Wrap(err)
		}
	}

	// update existing rows
	if err := db.Exec(`UPDATE attachments SET embedded_content = false WHERE embedded_content IS NULL`).Error; err != nil {
		return errs.Wrap(err)
	}

	// migration for email_attachments.is_inline
	// first add column as nullable
	if err := db.Exec(`ALTER TABLE email_attachments ADD COLUMN is_inline BOOLEAN`).Error; err != nil {
		// column might already exist, ignore error
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "duplicate") && !strings.Contains(errMsg, "already exists") {
			return errs.Wrap(err)
		}
	}

	// update existing rows - default to false (regular attachments)
	if err := db.Exec(`UPDATE email_attachments SET is_inline = false WHERE is_inline IS NULL`).Error; err != nil {
		return errs.Wrap(err)
	}

	// migration for allow_denies.headers
	// first add column as nullable
	if err := db.Exec(`ALTER TABLE allow_denies ADD COLUMN headers TEXT`).Error; err != nil {
		// column might already exist, ignore error
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "duplicate") && !strings.Contains(errMsg, "already exists") {
			return errs.Wrap(err)
		}
	}

	// update existing rows to have empty string default
	if err := db.Exec(`UPDATE allow_denies SET headers = '' WHERE headers IS NULL`).Error; err != nil {
		return errs.Wrap(err)
	}

	// migration for converting single webhook to multiple webhooks
	// migrate existing campaigns with webhook_id to campaign_webhooks junction table
	if err := db.Exec(`
		INSERT INTO campaign_webhooks (campaign_id, webhook_id, webhook_include_data, webhook_events)
		SELECT id, webhook_id, webhook_include_data, webhook_events
		FROM campaigns
		WHERE webhook_id IS NOT NULL
		AND NOT EXISTS (
			SELECT 1 FROM campaign_webhooks cw WHERE cw.campaign_id = campaigns.id AND cw.webhook_id = campaigns.webhook_id
		)
	`).Error; err != nil {
		// log but don't fail - this might be a re-run
		errMsg := strings.ToLower(err.Error())
		if !strings.Contains(errMsg, "duplicate") && !strings.Contains(errMsg, "unique constraint") {
			return errs.Wrap(err)
		}
	}

	return nil
}
