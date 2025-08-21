package model

type CampaignsStatView struct {
	Active   int64 `json:"active"`
	Upcoming int64 `json:"upcoming"`
	Finished int64 `json:"finished"`
}
