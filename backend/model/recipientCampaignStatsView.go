package model

type RecipientCampaignStatsView struct {
	CampaignsParticiated         int64 `json:"campaignsParticiated"`
	CampaignsTrackingPixelLoaded int64 `json:"campaignsTrackingPixelLoaded"`
	CampaignsPhishingPageLoaded  int64 `json:"campaignsPhishingPageLoaded"`
	CampaignsDataSubmitted       int64 `json:"campaignsDataSubmitted"`
	CampaignsReported            int64 `json:"campaignsReported"`
	RepeatLinkClicks             int64 `json:"repeatLinkClicks"`
	RepeatSubmissions            int64 `json:"repeatSubmissions"`
}
