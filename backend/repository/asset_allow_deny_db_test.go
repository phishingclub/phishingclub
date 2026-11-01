package repository

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
)

// list inserts an allow deny list with one IP range
func (f *assetServeFixture) list(name string, allowed bool, companyID *uuid.UUID) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	if err := f.db.Create(&database.AllowDeny{
		ID: &id, CreatedAt: &f.now, UpdatedAt: &f.now, CompanyID: companyID,
		Name: name, Cidrs: "10.0.0.0/8", Allowed: allowed,
	}).Error; err != nil {
		f.t.Fatalf("insert list: %v", err)
	}
	return id
}

func (f *assetServeFixture) link(assetID uuid.UUID, listIDs ...uuid.UUID) {
	f.t.Helper()
	ids := []*uuid.UUID{}
	for i := range listIDs {
		ids = append(ids, &listIDs[i])
	}
	if err := f.repo.ReplaceAllowDeny(context.Background(), &assetID, ids); err != nil {
		f.t.Fatalf("link: %v", err)
	}
}

func (f *assetServeFixture) linkCount(where string, arg any) int64 {
	f.t.Helper()
	var n int64
	f.db.Model(&database.AssetAllowDeny{}).Where(where, arg).Count(&n)
	return n
}

// TestAssetServeFilterFlagDomain checks the domain lookup tells which rows
// have lists without changing the headers it returns
func TestAssetServeFilterFlagDomain(t *testing.T) {
	f := newAssetServeFixture(t)
	ctx := context.Background()
	a := f.domain("a.test", nil)
	plain := f.asset(&a, nil, "plain.txt", "")
	f.header(plain, "X-Plain", "p")
	filtered := f.asset(&a, nil, "secret.txt", "text/x-secret")
	f.header(filtered, "X-One", "1")
	f.header(filtered, "X-Two", "2")
	l1 := f.list("one", false, nil)
	l2 := f.list("two", false, nil)
	f.link(filtered, l1, l2)

	got, err := f.repo.GetServeSettingsForDomainAsset(ctx, &a, "plain.txt")
	if err != nil || got == nil || len(got.FilteredAssetIDs) != 0 || len(got.Headers) != 1 {
		t.Fatalf("plain = %+v, %v", got, err)
	}
	got, err = f.repo.GetServeSettingsForDomainAsset(ctx, &a, "secret.txt")
	if err != nil || got == nil {
		t.Fatalf("secret: %v", err)
	}
	if !slices.Equal(got.FilteredAssetIDs, []uuid.UUID{filtered}) {
		t.Fatalf("filtered ids = %v", got.FilteredAssetIDs)
	}
	// two lists must not double the header rows
	if len(got.Headers) != 2 || got.ContentType != "text/x-secret" {
		t.Fatalf("secret settings = %+v", got)
	}

	ids, err := f.repo.GetFilteredAssetIDsForDomainAsset(ctx, &a, "secret.txt")
	if err != nil || !slices.Equal(ids, []uuid.UUID{filtered}) {
		t.Fatalf("filtered only = %v, %v", ids, err)
	}
	ids, err = f.repo.GetFilteredAssetIDsForDomainAsset(ctx, &a, "plain.txt")
	if err != nil || len(ids) != 0 {
		t.Fatalf("filtered only plain = %v, %v", ids, err)
	}
	// path variants never match the stored row
	for _, p := range []string{"/secret.txt", "SECRET.txt", "./secret.txt", "x/../secret.txt"} {
		got, err := f.repo.GetServeSettingsForDomainAsset(ctx, &a, p)
		if err != nil || got != nil {
			t.Fatalf("variant %q = %+v, %v", p, got, err)
		}
	}
}

// TestAssetServeFilterFlagSharedCollision checks that the lists of every row
// stored for a shared file are returned, while the company row still gives
// the headers
func TestAssetServeFilterFlagSharedCollision(t *testing.T) {
	f := newAssetServeFixture(t)
	ctx := context.Background()
	companyID := f.company("ck1")
	companyAsset := f.asset(nil, &companyID, "doc.pdf", "application/x-company")
	f.header(companyAsset, "X-Company", "c")
	global := f.asset(nil, nil, "ck1/doc.pdf", "application/x-global")
	f.link(global, f.list("global deny", false, nil))

	got, err := f.repo.GetServeSettingsForSharedAsset(ctx, "ck1/doc.pdf")
	if err != nil || got == nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.AssetID != companyAsset || got.ContentType != "application/x-company" {
		t.Fatalf("picked %v %q", got.AssetID, got.ContentType)
	}
	if !slices.Equal(got.FilteredAssetIDs, []uuid.UUID{global}) {
		t.Fatalf("filtered ids = %v, want global row", got.FilteredAssetIDs)
	}

	f.link(companyAsset, f.list("company deny", false, &companyID))
	got, err = f.repo.GetServeSettingsForSharedAsset(ctx, "ck1/doc.pdf")
	if err != nil || got == nil || len(got.FilteredAssetIDs) != 2 ||
		!slices.Contains(got.FilteredAssetIDs, global) || !slices.Contains(got.FilteredAssetIDs, companyAsset) {
		t.Fatalf("both filtered = %+v, %v", got, err)
	}
	ids, err := f.repo.GetFilteredAssetIDsForSharedAsset(ctx, "ck1/doc.pdf")
	if err != nil || len(ids) != 2 {
		t.Fatalf("filtered only = %v, %v", ids, err)
	}
	lists, err := f.repo.GetAllowDenyByAssetIDs(ctx, ids)
	if err != nil || len(lists[global]) != 1 || len(lists[companyAsset]) != 1 {
		t.Fatalf("lists = %v, %v", lists, err)
	}
	if lists[global][0].Name.MustGet().String() != "global deny" {
		t.Fatalf("lists mixed between assets: %v", lists)
	}
	// a global row at another path is not part of the file
	ids, err = f.repo.GetFilteredAssetIDsForSharedAsset(ctx, "doc.pdf")
	if err != nil || len(ids) != 0 {
		t.Fatalf("other path = %v, %v", ids, err)
	}
}

// TestAssetAllowDenyLinks checks replacing, removing and deleting links
func TestAssetAllowDenyLinks(t *testing.T) {
	f := newAssetServeFixture(t)
	ctx := context.Background()
	one := f.asset(nil, nil, "one.txt", "")
	two := f.asset(nil, nil, "two.txt", "")
	l1 := f.list("l1", true, nil)
	l2 := f.list("l2", true, nil)
	l3 := f.list("l3", true, nil)

	f.link(one, l1, l2)
	f.link(two, l2)
	lists, err := f.repo.GetAllowDenyByAssetID(ctx, &one)
	if err != nil || len(lists) != 2 {
		t.Fatalf("one lists = %v, %v", lists, err)
	}
	// replacing sets exactly the new lists
	f.link(one, l3)
	lists, _ = f.repo.GetAllowDenyByAssetID(ctx, &one)
	if len(lists) != 1 || lists[0].ID.MustGet() != l3 {
		t.Fatalf("replaced lists = %v", lists)
	}
	// the same pair can not be stored twice
	if err := f.db.Create(&database.AssetAllowDeny{AssetID: &one, AllowDenyID: &l3}).Error; err == nil {
		t.Fatalf("duplicate link was stored")
	}
	// an empty replace removes all lists
	f.link(one)
	lists, err = f.repo.GetAllowDenyByAssetID(ctx, &one)
	if err != nil || len(lists) != 0 || lists == nil {
		t.Fatalf("cleared lists = %v, %v", lists, err)
	}

	// removing a list removes it from every asset
	f.link(one, l2)
	if err := f.repo.RemoveAllowDenyByAllowDenyID(ctx, &l2); err != nil {
		t.Fatalf("remove list: %v", err)
	}
	if n := f.linkCount("allow_deny_id = ?", l2); n != 0 {
		t.Fatalf("links left for removed list: %d", n)
	}

	// deleting an asset removes its links and keeps the others
	f.link(one, l1)
	f.link(two, l1)
	if err := f.repo.DeleteByID(ctx, &one); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := f.linkCount("asset_id = ?", one); n != 0 {
		t.Fatalf("links left for deleted asset: %d", n)
	}
	if n := f.linkCount("asset_id = ?", two); n != 1 {
		t.Fatalf("links of other asset = %d", n)
	}
}

// TestAssetListHasAllowDeny checks the asset listings mark the assets that
// have allow deny lists and leave the others unmarked
func TestAssetListHasAllowDeny(t *testing.T) {
	f := newAssetServeFixture(t)
	ctx := context.Background()
	c := f.company("acme")
	d := f.domain("a.test", &c)
	domainPlain := f.asset(&d, &c, "plain.txt", "")
	domainFiltered := f.asset(&d, &c, "secret.txt", "")
	globalPlain := f.asset(nil, nil, "g-plain.txt", "")
	globalFiltered := f.asset(nil, nil, "g-secret.txt", "")
	companyFiltered := f.asset(nil, &c, "c-secret.txt", "")
	l1 := f.list("one", true, nil)
	l2 := f.list("two", true, nil)
	// two lists on one asset must not list the asset twice
	f.link(domainFiltered, l1, l2)
	f.link(globalFiltered, l1)
	f.link(companyFiltered, l2)

	want := map[uuid.UUID]bool{
		domainPlain: false, domainFiltered: true,
		globalPlain: false, globalFiltered: true,
		companyFiltered: true,
	}
	check := func(name string, rows int) func(res []*model.Asset, err error) {
		return func(res []*model.Asset, err error) {
			t.Helper()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if len(res) != rows {
				t.Fatalf("%s: got %d rows, want %d", name, len(res), rows)
			}
			for _, a := range res {
				id := a.ID.MustGet()
				if a.HasAllowDeny != want[id] {
					t.Fatalf("%s: asset %s HasAllowDeny = %v", name, a.Path.MustGet().String(), a.HasAllowDeny)
				}
			}
		}
	}
	rows := func(r *model.Result[model.Asset], err error) ([]*model.Asset, error) {
		if err != nil {
			return nil, err
		}
		return r.Rows, nil
	}
	check("domain", 2)(rows(f.repo.GetAllByDomainAndContext(ctx, &d, &c, nil)))
	check("global", 2)(rows(f.repo.GetAllByGlobalContext(ctx, nil)))
	check("company", 1)(rows(f.repo.GetAllByCompanyContext(ctx, &c, nil)))
}
