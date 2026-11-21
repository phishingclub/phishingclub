package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"gorm.io/gorm"
)

// programStatsScope limits a program query to a company. Without a company it
// counts every company, like the dashboard campaign counts.
func (r *Program) programStatsScope(ctx context.Context, companyID *uuid.UUID) *gorm.DB {
	db := r.DB.WithContext(ctx).Model(&database.Program{})
	if companyID != nil {
		db = whereCompany(db, database.PROGRAM_TABLE, companyID)
	}
	return db
}

// CountByStatus counts the programs in scope per list status, with the same
// rules as the status filter of the program list, and the total.
func (r *Program) CountByStatus(
	ctx context.Context,
	companyID *uuid.UUID,
	now time.Time,
) (*model.ProgramsStatView, error) {
	out := &model.ProgramsStatView{}
	if err := r.programStatsScope(ctx, companyID).Count(&out.Total).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	for status, count := range map[string]*int64{
		model.ProgramStatusScheduled:      &out.Scheduled,
		model.ProgramStatusPaused:         &out.Paused,
		model.ProgramStatusActive:         &out.Active,
		model.ProgramStatusNeedsAttention: &out.NeedsAttention,
	} {
		db, err := r.whereListStatus(r.programStatsScope(ctx, companyID), status, now)
		if err != nil {
			return nil, err
		}
		if err := db.Count(count).Error; err != nil {
			return nil, errs.Wrap(err)
		}
	}
	return out, nil
}
