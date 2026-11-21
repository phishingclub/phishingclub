package service

import (
	"math/rand"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

// TestRecipientScheduleWindowStretches checks the window shown for a round
// that starts late on a Friday reaches into the next week, the same as the
// scheduler stretches the round, see programRoundEnd, and holds every time
// the scheduler draws
func TestRecipientScheduleWindowStretches(t *testing.T) {
	// monday to friday 09:00 to 17:00 UTC, a spread of one day
	p := &model.Program{
		TimeZone:            nullable.NewNullableWithValue("UTC"),
		ConstraintWeekDays:  nullable.NewNullableWithValue(*vo.NewCampaignWeekDaysMust(62)),
		ConstraintStartTime: nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("09:00")),
		ConstraintEndTime:   nullable.NewNullableWithValue(*vo.NewCampaignTimeConstraintMust("17:00")),
		SpreadMinutes:       nullable.NewNullableWithValue(24 * 60),
	}
	// Friday 20 Nov 2026 at 16:00, one sending hour left that day
	roundStart := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
	w := programRoundWindow(p, roundStart, roundStart)
	if w.FirstDay != "2026-11-20" || w.LastDay != "2026-11-23" {
		t.Fatalf("window %s to %s, want 2026-11-20 to 2026-11-23", w.FirstDay, w.LastDay)
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 200; i++ {
		day := programPickSendTime(p, roundStart, roundStart, rng).Format(model.ProgramDateLayout)
		if day < w.FirstDay || day > w.LastDay {
			t.Fatalf("drawn day %s is outside the window %s to %s", day, w.FirstDay, w.LastDay)
		}
	}
	// a round that starts in the morning holds a whole spread and is not stretched
	morning := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	if w := programRoundWindow(p, morning, morning); w.FirstDay != "2026-11-18" || w.LastDay != "2026-11-18" {
		t.Fatalf("morning window %s to %s", w.FirstDay, w.LastDay)
	}
}
