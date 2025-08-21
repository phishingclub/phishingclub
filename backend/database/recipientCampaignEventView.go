package database

// RecipientCampaignEventView is a view read-only model
type RecipientCampaignEventView struct {
	CampaignEvent

	Name         string // event name
	CampaignName string
}
