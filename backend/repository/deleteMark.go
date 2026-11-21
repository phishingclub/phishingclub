package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// markDeleting sets deleting_at of one row of the model when it is not set
func markDeleting(ctx context.Context, db *gorm.DB, row any, id *uuid.UUID, at time.Time) error {
	err := db.WithContext(ctx).
		Model(row).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Where(clause.Eq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		UpdateColumn("deleting_at", at.UTC()).Error
	return errs.Wrap(err)
}

// idsBeingDeleted returns up to limit ids of the model whose deleting_at is
// set, the oldest mark first, leaving out the ids in skip
func idsBeingDeleted(ctx context.Context, db *gorm.DB, row any, limit int, skip []*uuid.UUID) ([]*uuid.UUID, error) {
	return pluckUUIDs(db.WithContext(ctx).
		Model(row).
		Where(clause.Neq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Where(notInIDs(skip)).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "deleting_at"}}).
		Limit(limit), "id")
}

// countRowsWhere counts the rows of the model whose column equals id
func countRowsWhere(ctx context.Context, db *gorm.DB, row any, column string, id *uuid.UUID) (int64, error) {
	var n int64
	err := db.WithContext(ctx).
		Model(row).
		Where(clause.Eq{Column: clause.Column{Name: column}, Value: id}).
		Count(&n).Error
	return n, errs.Wrap(err)
}

// IsDeleting reports whether a company is marked as being deleted. A nil id
// is the shared view, which is never deleted.
func (r *Company) IsDeleting(ctx context.Context, id *uuid.UUID) (bool, error) {
	db := r.DB
	if id == nil {
		return false, nil
	}
	var n int64
	err := db.WithContext(ctx).
		Model(&database.Company{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Where(clause.Neq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Count(&n).Error
	if err != nil {
		return false, errs.Wrap(err)
	}
	return n > 0, nil
}

// companyBlocker is a table that holds rows of a company a company delete
// does not remove
type companyBlocker struct {
	row   any
	label string
}

// companyBlockers are the tables whose rows keep a company from being
// deleted. They hold settings and accounts an admin moves or removes first.
var companyBlockers = []companyBlocker{
	{&database.User{}, "users"},
	{&database.SMTPConfiguration{}, "SMTP configurations"},
	{&database.Proxy{}, "proxies"},
	{&database.OAuthProvider{}, "OAuth providers"},
	{&database.RemoteBrowser{}, "remote browsers"},
	{&database.ReportTemplate{}, "report templates"},
}

// GetDeleteBlockers names what a company still holds that its delete does not
// remove, empty when nothing blocks it
func (r *Company) GetDeleteBlockers(ctx context.Context, id *uuid.UUID) ([]string, error) {
	out := []string{}
	for _, b := range companyBlockers {
		n, err := countRowsWhere(ctx, r.DB, b.row, "company_id", id)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			out = append(out, b.label)
		}
	}
	// a proxy domain is only removed with its proxy configuration
	var proxyDomains int64
	err := r.DB.WithContext(ctx).
		Model(&database.Domain{}).
		Where(clause.Eq{Column: clause.Column{Name: "company_id"}, Value: id}).
		Where(clause.Eq{Column: clause.Column{Name: "type"}, Value: "proxy"}).
		Count(&proxyDomains).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if proxyDomains > 0 {
		out = append(out, "proxy domains")
	}
	return out, nil
}

// HasDomainNamed reports whether a company has a domain of the given name,
// in any case
func (r *Company) HasDomainNamed(ctx context.Context, id *uuid.UUID, name string) (bool, error) {
	var n int64
	err := r.DB.WithContext(ctx).
		Model(&database.Domain{}).
		Where(clause.Eq{Column: clause.Column{Name: "company_id"}, Value: id}).
		Where(clause.Expr{SQL: "LOWER(?) = LOWER(?)", Vars: []any{clause.Column{Name: "name"}, name}}).
		Count(&n).Error
	if err != nil {
		return false, errs.Wrap(err)
	}
	return n > 0, nil
}

// CountReportConfigUse counts the report delivery settings that send to a
// recipient group
func (rg *RecipientGroup) CountReportConfigUse(ctx context.Context, id *uuid.UUID) (int64, error) {
	return countRowsWhere(ctx, rg.DB, &database.CompanyReportConfig{}, "recipient_group_id", id)
}

// MarkDeleting marks a company as being deleted, a mark that is set is kept
func (r *Company) MarkDeleting(ctx context.Context, id *uuid.UUID, at time.Time) error {
	return markDeleting(ctx, r.DB, &database.Company{}, id, at)
}

// GetIDsBeingDeleted returns up to limit companies marked as being deleted,
// the oldest mark first, leaving out the ids in skip
func (r *Company) GetIDsBeingDeleted(ctx context.Context, limit int, skip []*uuid.UUID) ([]*uuid.UUID, error) {
	return idsBeingDeleted(ctx, r.DB, &database.Company{}, limit, skip)
}

// MarkDeleting marks a recipient group as being deleted, a mark that is set
// is kept
func (rg *RecipientGroup) MarkDeleting(ctx context.Context, id *uuid.UUID, at time.Time) error {
	return markDeleting(ctx, rg.DB, &database.RecipientGroup{}, id, at)
}

// GetIDsBeingDeleted returns up to limit recipient groups marked as being
// deleted, the oldest mark first, leaving out the ids in skip
func (rg *RecipientGroup) GetIDsBeingDeleted(ctx context.Context, limit int, skip []*uuid.UUID) ([]*uuid.UUID, error) {
	return idsBeingDeleted(ctx, rg.DB, &database.RecipientGroup{}, limit, skip)
}

// AnyBeingDeleted reports whether one of the groups is marked as being
// deleted. An empty list holds none.
func (rg *RecipientGroup) AnyBeingDeleted(ctx context.Context, ids []*uuid.UUID) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	var n int64
	err := rg.DB.WithContext(ctx).
		Model(&database.RecipientGroup{}).
		Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids)}).
		Where(clause.Neq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Count(&n).Error
	if err != nil {
		return false, errs.Wrap(err)
	}
	return n > 0, nil
}
