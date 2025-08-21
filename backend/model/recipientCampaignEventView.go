package model

type RecipientCampaignEvent struct {
	CampaignEvent

	// event name
	Name         string `json:"name"`
	CampaignName string `json:"campaignName"`
}
