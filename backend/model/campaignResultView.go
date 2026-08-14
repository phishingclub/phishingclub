package model

type CampaignResultView struct {
	Recipients          int64 `json:"recipients"`
	EmailsSent          int64 `json:"emailsSent"`
	TrackingPixelLoaded int64 `json:"trackingPixelLoaded"`
	WebsiteLoaded       int64 `json:"clickedLink"`
	SubmittedData       int64 `json:"submittedData"`
	Reported            int64 `json:"reported"`
	// training campaign funnel, zero for phishing campaigns
	TrainingStarted   int64 `json:"trainingStarted"`
	TrainingCompleted int64 `json:"trainingCompleted"`
}
