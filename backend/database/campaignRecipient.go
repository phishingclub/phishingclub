package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CAMPAIGN_RECIPIENT_TABLE_NAME = "campaign_recipients"
)

// CampaigReciever is gorm data model
// this model/table is primarily used to keep track of who and when should recieve a campaign
type CampaignRecipient struct {
	ID *uuid.UUID `gorm:"primary_key;not null;unique;type:uuid"`

	CreatedAt *time.Time `gorm:"not null;index;"`
	UpdatedAt *time.Time `gorm:"not null;index;"`

	Campaign   *Campaign
	CampaignID *uuid.UUID `gorm:"not null;type:uuid;uniqueIndex:idx_campaign_recipients_campaign_id_recipient_id;index:idx_campaign_recipients_campaign_template,priority:1;"`

	// CancelledAt *time.Time `gorm:"index;"`
	CancelledAt *time.Time `gorm:"index;"`

	// when it should be send
	SendAt *time.Time `gorm:"index;"`

	// when it was last attempted send
	LastAttemptAt *time.Time `gorm:"index;"`

	// when it was sent
	SentAt *time.Time `gorm:"index;"`

	// self-managed
	SelfManaged bool `gorm:"not null;default:false;"`

	// AnonymizedID is the stable pseudonym for this recipient in this campaign.
	// For an anonymous campaign it is assigned at materialization and stamped on
	// every event so events carry no identity. For a normal campaign it is
	// assigned at close by the anonymization sweep.
	AnonymizedID *uuid.UUID `gorm:"type:uuid;"`
	Recipient    *Recipient
	// A null recipientID means that the data has been anonymized
	RecipientID *uuid.UUID `gorm:"type:uuid;index;uniqueIndex:idx_campaign_recipients_campaign_id_recipient_id;"`

	// Position and Department are snapshotted from the recipient at
	// materialization so grouped statistics survive after the recipient relation
	// is severed at anonymization. Populated only for anonymous campaigns.
	Position   string `gorm:";"`
	Department string `gorm:";"`

	// CampaignTemplateID is the template this recipient was given from the
	// campaign's template pool. Null on rows created before pools existed, which
	// use campaigns.campaign_template_id instead. Indexed together with the
	// campaign, which serves per campaign reads such as filling rows that have no
	// template and checking how many templates a campaign used.
	CampaignTemplateID *uuid.UUID `gorm:"type:uuid;index:idx_campaign_recipients_campaign_template,priority:2"`

	// Language is the language this recipient is sent and shown, set when the
	// row is planned and never changed after it is sent. Null on older rows,
	// which use the template language. Not indexed, the campaign indexes cover
	// every read.
	Language *string `gorm:"type:varchar(16)"`

	// NotableEventID is the most notable event for this recipient
	NotableEvent   *Event     `gorm:"foreignKey:NotableEventID;references:ID"`
	NotableEventID *uuid.UUID `gorm:"type:uuid;index"`

	// LureCode replaces the campaign recipient UUID in a lure URL, for example
	// https://example.com/4H7K9QM2XR3T. Stored and matched byte for byte, so an
	// operator picking Special-42 gets that link verbatim.
	//
	// Releasing sets this to null rather than flagging it, which keeps a
	// reclaimed code from showing against its former owner and lets the unique
	// index below need nothing but IS NOT NULL.
	LureCode *string `gorm:"type:varchar(64)"`
	// LureCodeCustom marks a code the operator set by hand, which cannot be told
	// apart from a generated one by shape.
	LureCodeCustom bool `gorm:"not null;default:false"`

	// MessageID is the Message-ID header of the email sent to this recipient,
	// without angle brackets. A report inbox matches a reported email on it.
	MessageID *string `gorm:"type:varchar(512);index"`
}

func (CampaignRecipient) TableName() string {
	return CAMPAIGN_RECIPIENT_TABLE_NAME
}

// lureCodeIndexName is the partial unique index backing lure code allocation.
const lureCodeIndexName = "idx_campaign_recipients_lure_code"

// lureCodeCustomIndexName is the index answering whether a campaign carries any
// operator set code.
const lureCodeCustomIndexName = "idx_campaign_recipients_lure_code_custom"

// Migrate creates the indexes the lure code feature reads.
//
// The unique predicate must exclude rows carrying no code, or every recipient in
// a query mode campaign enters the index under the same value and the second
// insert fails. It covers reuse too, since releasing a code nulls it and the row
// leaves the index.
//
// Raw SQL rather than gorm index tags, because the generic migrator drops the
// WHERE clause and would silently build a full unique index.
func (CampaignRecipient) Migrate(db *gorm.DB) error {
	createUnique := func() error {
		return ensureIndex(db, lureCodeIndexName, `CREATE UNIQUE INDEX `+lureCodeIndexName+`
			ON campaign_recipients (lure_code)
			WHERE lure_code IS NOT NULL`)
	}
	if err := createUnique(); err != nil {
		// creation fails when two rows already hold the same code. keep the first
		// row for each and release the rest. only after a failure, because the
		// scan and grouping are too costly to pay on every startup.
		if dedupErr := db.Exec(`UPDATE campaign_recipients SET lure_code = NULL
			WHERE lure_code IS NOT NULL
			  AND rowid NOT IN (
			    SELECT MIN(rowid) FROM campaign_recipients
			    WHERE lure_code IS NOT NULL
			    GROUP BY lure_code
			  )`).Error; dedupErr != nil {
			return dedupErr
		}
		if err := createUnique(); err != nil {
			return err
		}
	}

	// only rows with an operator set code enter the index, rather than every
	// recipient ever created. sqlite uses a partial index only where the query
	// repeats the predicate, so HasCustomLureCodesByCampaignID writes the same
	// lure_code_custom = 1 literal. a bare column predicate would not match.
	if err := ensureIndex(db, lureCodeCustomIndexName, `CREATE INDEX `+lureCodeCustomIndexName+`
		ON campaign_recipients (campaign_id)
		WHERE lure_code_custom = 1`); err != nil {
		return err
	}

	// a program finds the sends of a person by pseudonym when the recipient link
	// is gone. Only rows with a pseudonym enter the index, a lookup by value
	// implies the IS NOT NULL predicate so sqlite can use it.
	if err := ensureIndex(db, "idx_campaign_recipients_anonymized_id", `CREATE INDEX idx_campaign_recipients_anonymized_id
		ON campaign_recipients (anonymized_id)
		WHERE anonymized_id IS NOT NULL`); err != nil {
		return err
	}

	// the identity removal of anonymous programs looks for sent rows that still
	// carry both the person and the pseudonym. RemoveIdentityOfSendsBefore
	// repeats both predicates so sqlite uses this index.
	if err := ensureIndex(db, "idx_campaign_recipients_identity_sent", `CREATE INDEX idx_campaign_recipients_identity_sent
		ON campaign_recipients (sent_at)
		WHERE recipient_id IS NOT NULL AND anonymized_id IS NOT NULL`); err != nil {
		return err
	}

	// the sender reads the rows waiting to be sent. Only those rows enter the
	// index, so the read does not walk every row sent in the past.
	// GetUnsendRecipientsForSending repeats both predicates.
	if err := ensureIndex(db, "idx_campaign_recipients_unsent", `CREATE INDEX idx_campaign_recipients_unsent
		ON campaign_recipients (send_at)
		WHERE sent_at IS NULL AND cancelled_at IS NULL`); err != nil {
		return err
	}

	// a program cancels and moves the rows of its campaigns that are neither
	// sent nor cancelled, of all its people or of some. Only those rows enter
	// the index. sent_at and cancelled_at lead the key although they are always
	// null in it: a query that states both as IS NULL then matches two key
	// columns, so sqlite picks this index over the single column sent_at and
	// cancelled_at indexes, which hold every row, even without statistics. The
	// recipient follows the campaign, so the rows of some people are found
	// without reading the table.
	if err := ensureIndex(db, "idx_campaign_recipients_unsent_campaign", `CREATE INDEX idx_campaign_recipients_unsent_campaign
		ON campaign_recipients (sent_at, cancelled_at, campaign_id, recipient_id)
		WHERE sent_at IS NULL AND cancelled_at IS NULL`); err != nil {
		return err
	}

	// the sent rows of a campaign that name a person, by when they were sent.
	// The facts of a program count them to find a removed send, and the
	// program timeline counts them per day within a time range of each
	// campaign. Both reads repeat the predicates, so they read this index only
	// and not the table rows. A send that names nobody is not counted by
	// either. The program code keeps a pseudonym on every send whose person
	// link it clears, so such rows come only from older data.
	if err := db.Exec(`DROP INDEX IF EXISTS idx_campaign_recipients_campaign_sent`).Error; err != nil {
		return err
	}
	if err := ensureIndex(db, "idx_campaign_recipients_campaign_sent_at", `CREATE INDEX idx_campaign_recipients_campaign_sent_at
		ON campaign_recipients (campaign_id, sent_at)
		WHERE sent_at IS NOT NULL AND (recipient_id IS NOT NULL OR anonymized_id IS NOT NULL)`); err != nil {
		return err
	}

	// the template mixing of a program counts the sends of its campaigns
	// planned in a time window. The read repeats the cancelled_at IS NULL
	// predicate, so it reads only the rows in the window of each campaign.
	return ensureIndex(db, "idx_campaign_recipients_campaign_send_at", `CREATE INDEX idx_campaign_recipients_campaign_send_at
		ON campaign_recipients (campaign_id, send_at)
		WHERE cancelled_at IS NULL`)
}

// ensureIndex creates an index, replacing whatever carries the name when its
// definition differs.
//
// The whole definition is compared, not just existence: an index left by an
// earlier release keeps working and goes unnoticed, while a predicate that no
// longer matches the query stops the planner using it at all. The ddl must omit
// IF NOT EXISTS, because sqlite drops those words before storing the statement
// and the two would never compare equal.
func ensureIndex(db *gorm.DB, name string, ddl string) error {
	// through the underlying pool rather than gorm, whose Row returns nil when it
	// could not build the statement and would panic here during startup
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	var definition sql.NullString
	err = sqlDB.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`,
		name,
	).Scan(&definition)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		// an index sqlite created for itself carries no definition and is not ours
		if !definition.Valid {
			return nil
		}
		if normalizeSQL(definition.String) == normalizeSQL(ddl) {
			return nil
		}
		if err := db.Exec(`DROP INDEX IF EXISTS ` + name).Error; err != nil {
			return err
		}
	}
	return db.Exec(ddl).Error
}

// normalizeSQL collapses whitespace so a statement read back from sqlite_master
// compares equal to the indented literal it was created from.
func normalizeSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
