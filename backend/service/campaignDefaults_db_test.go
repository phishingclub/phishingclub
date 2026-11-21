package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"gorm.io/gorm"
)

func boolPtr(v bool) *bool { return &v }

func newCampaignDefaultsService(t *testing.T) (*CampaignDefaults, *companyScopeFixture) {
	t.Helper()
	f := newCompanyScopeFixture(t)
	if err := f.db.AutoMigrate(&database.CampaignDefaults{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := (&database.CampaignDefaults{}).Migrate(f.db); err != nil {
		t.Fatalf("extra migrate: %v", err)
	}
	svc := &CampaignDefaults{
		Common:                     f.common(),
		CampaignDefaultsRepository: &repository.CampaignDefaults{DB: f.db},
		CompanyRepository:          &repository.Company{DB: f.db},
	}
	return svc, f
}

// TestCampaignDefaultsResolve checks a company value wins, an unset company
// field falls back to the global one, and another company only sees global
func TestCampaignDefaultsResolve(t *testing.T) {
	svc, f := newCampaignDefaultsService(t)

	got, err := svc.GetResolved(f.ctx, f.session, &f.c1)
	if err != nil {
		t.Fatalf("resolve empty: %v", err)
	}
	if got.CloseAfterDays != nil || got.IsAnonymous != nil || len(got.Sources) != 0 {
		t.Fatalf("nothing stored should resolve empty, got %+v", got)
	}

	if _, err := svc.Upsert(f.ctx, f.session, nil, &model.CampaignDefaults{
		CloseAfterDays:     intPtr(14),
		AnonymizeAfterDays: intPtr(30),
		SaveSubmittedData:  boolPtr(true),
	}); err != nil {
		t.Fatalf("upsert global: %v", err)
	}
	if _, err := svc.Upsert(f.ctx, f.session, &f.c1, &model.CampaignDefaults{
		CloseAfterDays: intPtr(7),
		IsAnonymous:    boolPtr(true),
	}); err != nil {
		t.Fatalf("upsert company: %v", err)
	}

	got, err = svc.GetResolved(f.ctx, f.session, &f.c1)
	if err != nil {
		t.Fatalf("resolve c1: %v", err)
	}
	if *got.CloseAfterDays != 7 || got.Sources["closeAfterDays"] != model.CampaignDefaultsSourceCompany {
		t.Fatalf("company close should win, got %+v", got)
	}
	if *got.AnonymizeAfterDays != 30 || got.Sources["anonymizeAfterDays"] != model.CampaignDefaultsSourceGlobal {
		t.Fatalf("anonymize should come from global, got %+v", got)
	}
	if got.DataAnonymizeAfterDays != nil {
		t.Fatalf("data anonymize unset on both levels, got %v", *got.DataAnonymizeAfterDays)
	}
	// fields resolve one by one, the campaign form turns data saving off
	// for an anonymous campaign
	if !*got.IsAnonymous || !*got.SaveSubmittedData || got.Sources["saveSubmittedData"] != model.CampaignDefaultsSourceGlobal {
		t.Fatalf("anonymous should come from the company and save data from global, got %+v", got)
	}

	got, err = svc.GetResolved(f.ctx, f.session, &f.c2)
	if err != nil {
		t.Fatalf("resolve c2: %v", err)
	}
	if *got.CloseAfterDays != 14 || got.IsAnonymous != nil || !*got.SaveSubmittedData {
		t.Fatalf("c2 should only see global, got %+v", got)
	}

	// a second upsert updates the same row, a nil field clears it
	if _, err := svc.Upsert(f.ctx, f.session, &f.c1, &model.CampaignDefaults{
		AnonymizeAfterDays: intPtr(0),
	}); err != nil {
		t.Fatalf("update company: %v", err)
	}
	got, err = svc.GetResolved(f.ctx, f.session, &f.c1)
	if err != nil {
		t.Fatalf("resolve c1 again: %v", err)
	}
	if *got.CloseAfterDays != 14 || *got.AnonymizeAfterDays != 0 || got.IsAnonymous != nil {
		t.Fatalf("update should replace the company row, got %+v", got)
	}
	var n int64
	f.db.Model(&database.CampaignDefaults{}).Count(&n)
	if n != 2 {
		t.Fatalf("want one global and one company row, got %d", n)
	}

	// deleting the company row falls back to global
	if err := svc.DeleteByCompanyID(f.ctx, f.session, &f.c1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	stored, err := svc.GetByCompanyID(f.ctx, f.session, &f.c1)
	if err != nil || stored != nil {
		t.Fatalf("company row should be gone, got %+v %v", stored, err)
	}
	stored, err = svc.GetByCompanyID(f.ctx, f.session, nil)
	if err != nil || stored == nil || *stored.CloseAfterDays != 14 {
		t.Fatalf("global row should stay, got %+v %v", stored, err)
	}
}

// TestCampaignDefaultsGuards checks validation, unknown companies and the
// permission check
func TestCampaignDefaultsGuards(t *testing.T) {
	svc, f := newCampaignDefaultsService(t)

	for name, in := range map[string]*model.CampaignDefaults{
		"negative days":            {CloseAfterDays: intPtr(-1)},
		"too many days":            {DataAnonymizeAfterDays: intPtr(model.CampaignDefaultsMaxDays + 1)},
		"anonymous saves the data": {IsAnonymous: boolPtr(true), SaveSubmittedData: boolPtr(true)},
	} {
		if _, err := svc.Upsert(f.ctx, f.session, &f.c1, in); !isValidationError(err) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}

	unknown := uuid.New()
	if _, err := svc.Upsert(f.ctx, f.session, &unknown, &model.CampaignDefaults{CloseAfterDays: intPtr(1)}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown company: want not found, got %v", err)
	}

	user := sessionWithRole(data.RoleCompanyUser)
	if _, err := svc.Upsert(f.ctx, user, nil, &model.CampaignDefaults{}); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("upsert without permission: got %v", err)
	}
	if _, err := svc.GetResolved(f.ctx, user, &f.c1); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("resolve without permission: got %v", err)
	}
	if _, err := svc.GetByCompanyID(f.ctx, user, nil); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("get without permission: got %v", err)
	}
	if err := svc.DeleteByCompanyID(f.ctx, user, nil); !errors.Is(err, errs.ErrAuthorizationFailed) {
		t.Fatalf("delete without permission: got %v", err)
	}
}

// TestCampaignDefaultsConcurrentFirstSave checks saves that race to create the
// first row of a scope all succeed and leave one row
func TestCampaignDefaultsConcurrentFirstSave(t *testing.T) {
	svc, f := newCampaignDefaultsService(t)
	for _, scope := range []*uuid.UUID{nil, &f.c1} {
		const n = 8
		errc := make(chan error, n)
		for i := 0; i < n; i++ {
			days := i
			go func() {
				_, err := svc.Upsert(f.ctx, f.session, scope, &model.CampaignDefaults{CloseAfterDays: intPtr(days)})
				errc <- err
			}()
		}
		for i := 0; i < n; i++ {
			if err := <-errc; err != nil {
				t.Fatalf("scope %v: concurrent save failed: %v", scope, err)
			}
		}
	}
	var n int64
	f.db.Model(&database.CampaignDefaults{}).Count(&n)
	if n != 2 {
		t.Fatalf("want one global and one company row, got %d", n)
	}
}
