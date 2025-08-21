package model

type CampaignResultView struct {
	Recipients          int64 `json:"recipients"`
	EmailsSent          int64 `json:"emailsSent"`
	TrackingPixelLoaded int64 `json:"trackingPixelLoaded"`
	WebsiteLoaded       int64 `json:"clickedLink"`
	SubmittedData       int64 `json:"submittedData"`
}
