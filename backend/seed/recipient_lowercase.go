package seed

import (
	"github.com/go-errors/errors"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// RecipientLowercaseReport summarises a lowercase-recipient-emails run.
type RecipientLowercaseReport struct {
	GroupsProcessed  int
	DuplicateGroups  int
	RecipientsMerged int
	EmailsLowercased int
}

// LowercaseRecipientEmails normalises every recipient email to lowercase and
// merges recipients that are the same address apart from casing into one
// canonical lowercased recipient. for each group the already lowercased row is
// kept, otherwise the oldest row is kept and lowercased. every reference to a
// merged recipient (campaign membership, group membership, events, device
// codes) is repointed to the survivor, and any membership that would become a
// duplicate is collapsed to one. the whole run is one transaction and is safe
// to run more than once.
//
// when dryRun is true the work is performed in a transaction that is rolled
// back, so the report reflects exactly what would change without writing any
// changes.
func LowercaseRecipientEmails(db *gorm.DB, logger *zap.SugaredLogger, dryRun bool) (*RecipientLowercaseReport, error) {
	report := &RecipientLowercaseReport{}
	tx := db.Begin()
	if tx.Error != nil {
		return nil, errs.Wrap(tx.Error)
	}
	if err := lowercaseRecipientEmailsInTx(tx, logger, report); err != nil {
		tx.Rollback()
		return nil, errs.Wrap(errors.Errorf("lowercase recipient emails migration failed: %w", err))
	}
	if dryRun {
		tx.Rollback()
		logger.Infow("lowercase-recipient-emails dry run, no changes written",
			"groupsProcessed", report.GroupsProcessed,
			"duplicateGroups", report.DuplicateGroups,
			"recipientsToMerge", report.RecipientsMerged,
			"emailsToLowercase", report.EmailsLowercased,
		)
		return report, nil
	}
	if err := tx.Commit().Error; err != nil {
		return nil, errs.Wrap(err)
	}
	logger.Infow("lowercase-recipient-emails migration completed",
		"groupsProcessed", report.GroupsProcessed,
		"duplicateGroups", report.DuplicateGroups,
		"recipientsMerged", report.RecipientsMerged,
		"emailsLowercased", report.EmailsLowercased,
	)
	return report, nil
}

// lowercaseRecipientEmailsInTx performs the normalisation and merge work on the
// given transaction, filling in the report as it goes.
func lowercaseRecipientEmailsInTx(tx *gorm.DB, logger *zap.SugaredLogger, report *RecipientLowercaseReport) error {
	// find every lowercased email that needs work, either because more than
	// one recipient shares it or because the single recipient is not stored
	// lowercased
	var keys []struct {
		LowerEmail string `gorm:"column:lower_email"`
	}
	err := tx.Raw(
		"SELECT LOWER(email) AS lower_email FROM " + database.RECIPIENT_TABLE +
			" WHERE email IS NOT NULL AND email != ''" +
			" GROUP BY LOWER(email)" +
			" HAVING COUNT(*) > 1 OR SUM(CASE WHEN email = LOWER(email) THEN 1 ELSE 0 END) < COUNT(*)",
	).Scan(&keys).Error
	if err != nil {
		return errs.Wrap(err)
	}

	for _, k := range keys {
		report.GroupsProcessed++
		// load the group, ordered so the survivor is first: an already
		// lowercased row wins, then the oldest row
		var rows []struct {
			ID    string `gorm:"column:id"`
			Email string `gorm:"column:email"`
		}
		err := tx.Raw(
			"SELECT id, email FROM "+database.RECIPIENT_TABLE+
				" WHERE email IS NOT NULL AND email != '' AND LOWER(email) = ?"+
				" ORDER BY CASE WHEN email = LOWER(email) THEN 0 ELSE 1 END ASC, created_at ASC, id ASC",
			k.LowerEmail,
		).Scan(&rows).Error
		if err != nil {
			return errs.Wrap(err)
		}
		if len(rows) == 0 {
			continue
		}
		survivor := rows[0]
		if len(rows) > 1 {
			report.DuplicateGroups++
		}
		for _, nonSurvivor := range rows[1:] {
			if err := mergeRecipient(tx, nonSurvivor.ID, survivor.ID); err != nil {
				return errs.Wrap(err)
			}
			report.RecipientsMerged++
		}
		if survivor.Email != k.LowerEmail {
			if err := tx.Exec(
				"UPDATE "+database.RECIPIENT_TABLE+" SET email = ? WHERE id = ?",
				k.LowerEmail, survivor.ID,
			).Error; err != nil {
				return errs.Wrap(err)
			}
			report.EmailsLowercased++
		}
		logger.Debugw("processed recipient email group",
			"email", k.LowerEmail,
			"survivor", survivor.ID,
			"merged", len(rows)-1,
		)
	}
	return nil
}

// mergeRecipient repoints every reference from fromID to toID, collapsing any
// membership that would otherwise duplicate, then deletes the fromID recipient.
// references are repointed before the delete so no foreign key is left dangling.
func mergeRecipient(tx *gorm.DB, fromID string, toID string) error {
	// campaign membership is unique on (campaign_id, recipient_id), so first
	// drop the rows that the survivor already has for the same campaign
	if err := tx.Exec(
		"DELETE FROM "+database.CAMPAIGN_RECIPIENT_TABLE_NAME+
			" WHERE recipient_id = ? AND campaign_id IN"+
			" (SELECT campaign_id FROM "+database.CAMPAIGN_RECIPIENT_TABLE_NAME+
			" WHERE recipient_id = ?)",
		fromID, toID,
	).Error; err != nil {
		return err
	}
	if err := tx.Exec(
		"UPDATE "+database.CAMPAIGN_RECIPIENT_TABLE_NAME+
			" SET recipient_id = ? WHERE recipient_id = ?",
		toID, fromID,
	).Error; err != nil {
		return err
	}

	// group membership is unique on (recipient_id, recipient_group_id)
	if err := tx.Exec(
		"DELETE FROM "+database.RECIPIENT_GROUP_RECIPIENT_TABLE+
			" WHERE recipient_id = ? AND recipient_group_id IN"+
			" (SELECT recipient_group_id FROM "+database.RECIPIENT_GROUP_RECIPIENT_TABLE+
			" WHERE recipient_id = ?)",
		fromID, toID,
	).Error; err != nil {
		return err
	}
	if err := tx.Exec(
		"UPDATE "+database.RECIPIENT_GROUP_RECIPIENT_TABLE+
			" SET recipient_id = ? WHERE recipient_id = ?",
		toID, fromID,
	).Error; err != nil {
		return err
	}

	// device codes are unique on (campaign_id, recipient_id)
	if err := tx.Exec(
		"DELETE FROM "+database.MICROSOFT_DEVICE_CODE_TABLE+
			" WHERE recipient_id = ? AND campaign_id IN"+
			" (SELECT campaign_id FROM "+database.MICROSOFT_DEVICE_CODE_TABLE+
			" WHERE recipient_id = ?)",
		fromID, toID,
	).Error; err != nil {
		return err
	}
	if err := tx.Exec(
		"UPDATE "+database.MICROSOFT_DEVICE_CODE_TABLE+
			" SET recipient_id = ? WHERE recipient_id = ?",
		toID, fromID,
	).Error; err != nil {
		return err
	}

	// events have no uniqueness on the recipient, so keep them all and repoint
	if err := tx.Exec(
		"UPDATE "+database.CAMPAIGN_EVENT_TABLE+
			" SET recipient_id = ? WHERE recipient_id = ?",
		toID, fromID,
	).Error; err != nil {
		return err
	}

	// the merged recipient now has no references and can be removed
	if err := tx.Exec(
		"DELETE FROM "+database.RECIPIENT_TABLE+" WHERE id = ?",
		fromID,
	).Error; err != nil {
		return err
	}
	return nil
}
