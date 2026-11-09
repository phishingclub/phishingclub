package database

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CAMPAIGN_TEMPLATE_POOL_TABLE = "campaign_template_pool"
)

// CampaignTemplatePool holds the templates a campaign picks from. Each campaign
// recipient is given one of them when the campaign is scheduled. The entry at the
// lowest position matches campaigns.campaign_template_id.
type CampaignTemplatePool struct {
	CampaignID         *uuid.UUID `gorm:"not null;type:uuid;primaryKey;"`
	CampaignTemplateID *uuid.UUID `gorm:"not null;index;type:uuid;primaryKey;"`
	Position           int        `gorm:"not null;default:0"`
}

func (CampaignTemplatePool) TableName() string {
	return CAMPAIGN_TEMPLATE_POOL_TABLE
}

// Migrate gives every campaign that has a template but no pool rows a pool of
// that one template. Safe to run on every start.
func (CampaignTemplatePool) Migrate(db *gorm.DB) error {
	return db.Exec(`INSERT INTO campaign_template_pool (campaign_id, campaign_template_id, position)
		SELECT c.id, c.campaign_template_id, 0
		FROM campaigns c
		WHERE c.campaign_template_id IS NOT NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM campaign_template_pool p WHERE p.campaign_id = c.id
		  )`).Error
}
