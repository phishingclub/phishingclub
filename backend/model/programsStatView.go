package model

// ProgramsStatView is the number of programs in each state shown on the
// dashboard. Active and NeedsAttention split the running programs the same
// way the program list does, so each program is counted once.
type ProgramsStatView struct {
	Active         int64 `json:"active"`
	Scheduled      int64 `json:"scheduled"`
	Paused         int64 `json:"paused"`
	NeedsAttention int64 `json:"needsAttention"`
	// Total counts every program in scope, drafts and closed included
	Total int64 `json:"total"`
}
