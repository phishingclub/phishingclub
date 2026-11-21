package model

// CampaignsStatView holds the dashboard counts of one company or of all
// companies
type CampaignsStatView struct {
	// Active is sending now, paused campaigns left out
	Active int64 `json:"active"`
	// SelfManaged is the part of Active without a delivery window
	SelfManaged int64 `json:"selfManaged"`
	Upcoming    int64 `json:"upcoming"`
	Finished    int64 `json:"finished"`
	// Paused is open with sending paused
	Paused int64 `json:"paused"`
	// FailedSending counts the failed sends of the last FailedSendingDays days
	FailedSending     int64 `json:"failedSending"`
	FailedSendingDays int   `json:"failedSendingDays"`
	// ReportInboxErrors counts the turned on report inboxes whose last read failed
	ReportInboxErrors int64 `json:"reportInboxErrors"`
}
