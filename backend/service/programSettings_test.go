package service

import (
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

func cloneTestSource(sourceCompany *uuid.UUID) (*programCloneSource, map[string]uuid.UUID) {
	ids := map[string]uuid.UUID{}
	for _, k := range []string{"globalTpl", "companyTpl", "trainingTpl", "group", "denyGlobal", "evasionCompany", "filterGlobal", "filterCompany", "hookGlobal", "hookCompany"} {
		ids[k] = uuid.New()
	}
	company := nullable.NewNullNullable[uuid.UUID]()
	if sourceCompany != nil {
		company.Set(*sourceCompany)
	}
	global := nullable.NewNullNullable[uuid.UUID]()
	id := func(k string) *uuid.UUID { v := ids[k]; return &v }
	p := &model.Program{
		ID:                  nullable.NewNullableWithValue(uuid.New()),
		Name:                nullable.NewNullableWithValue(*vo.NewString64Must("source")),
		CompanyID:           company,
		OrderMode:           nullable.NewNullableWithValue(model.ProgramOrderShuffle),
		LoopMode:            nullable.NewNullableWithValue(model.ProgramLoopLoop),
		StepIntervalMinutes: nullable.NewNullableWithValue(10080),
		TimeZone:            nullable.NewNullableWithValue("Europe/Copenhagen"),
		IsAnonymous:         nullable.NewNullableWithValue(true),
		Steps: []*model.ProgramStep{
			{ID: nullable.NewNullableWithValue(uuid.New()), CampaignTemplateID: nullable.NewNullableWithValue(ids["globalTpl"])},
			{ID: nullable.NewNullableWithValue(uuid.New()), CampaignTemplateID: nullable.NewNullableWithValue(ids["companyTpl"])},
			{ID: nullable.NewNullableWithValue(uuid.New()), CampaignTemplateID: nullable.NewNullableWithValue(ids["trainingTpl"])},
		},
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{id("group")}),
		DenyPageID:        nullable.NewNullableWithValue(ids["denyGlobal"]),
		EvasionPageID:     nullable.NewNullableWithValue(ids["evasionCompany"]),
		AllowDenyIDs:      nullable.NewNullableWithValue([]*uuid.UUID{id("filterGlobal"), id("filterCompany")}),
		Webhooks: nullable.NewNullableWithValue([]*model.CampaignWebhook{
			{WebhookID: nullable.NewNullableWithValue(ids["hookGlobal"]), WebhookIncludeData: nullable.NewNullableWithValue("basic"), WebhookEvents: nullable.NewNullableWithValue(8)},
			{WebhookID: nullable.NewNullableWithValue(ids["hookCompany"])},
		}),
	}
	src := &programCloneSource{
		Program: p,
		Templates: map[uuid.UUID]programCloneItem{
			ids["globalTpl"]:   {Name: "global", Company: global},
			ids["companyTpl"]:  {Name: "company", Company: company},
			ids["trainingTpl"]: {Name: "training", Company: company},
		},
		Groups:    map[uuid.UUID]programCloneItem{ids["group"]: {Name: "group", Company: company}},
		Pages:     map[uuid.UUID]programCloneItem{ids["denyGlobal"]: {Name: "deny", Company: global}, ids["evasionCompany"]: {Name: "evasion", Company: company}},
		AllowDeny: map[uuid.UUID]programCloneItem{ids["filterGlobal"]: {Name: "fg", Company: global}, ids["filterCompany"]: {Name: "fc", Company: company}},
		Webhooks:  map[uuid.UUID]programCloneItem{ids["hookGlobal"]: {Name: "hg", Company: global}, ids["hookCompany"]: {Name: "hc", Company: company}},
	}
	return src, ids
}

func droppedKinds(d []ProgramCloneDropped) []string {
	out := []string{}
	for _, x := range d {
		out = append(out, x.Kind+":"+x.Name)
	}
	sort.Strings(out)
	return out
}

func TestBuildProgramCloneSameCompany(t *testing.T) {
	companyA := uuid.New()
	src, ids := cloneTestSource(&companyA)
	clone, dropped := BuildProgramClone(src, *vo.NewString64Must("copy"), &companyA)
	if len(dropped) != 0 {
		t.Fatalf("same company clone must keep everything, dropped %v", droppedKinds(dropped))
	}
	if clone.CompanyID.MustGet() != companyA || clone.Name.MustGet().String() != "copy" {
		t.Fatal("clone must take the new name and the target company")
	}
	if len(clone.Steps) != 3 {
		t.Fatalf("want 3 steps, got %d", len(clone.Steps))
	}
	for i, st := range clone.Steps {
		if st.ID.IsSpecified() {
			t.Fatal("cloned steps must not carry the source step id")
		}
		if st.Position.MustGet() != i {
			t.Fatal("positions must be renumbered")
		}
	}
	if len(clone.RecipientGroupIDs.MustGet()) != 1 || *clone.RecipientGroupIDs.MustGet()[0] != ids["group"] {
		t.Fatal("same company clone keeps the groups")
	}
	if clone.EvasionPageID.MustGet() != ids["evasionCompany"] || clone.DenyPageID.MustGet() != ids["denyGlobal"] {
		t.Fatal("pages must be kept")
	}
	if len(clone.AllowDenyIDs.MustGet()) != 2 || len(clone.Webhooks.MustGet()) != 2 {
		t.Fatal("filters and webhooks must be kept")
	}
	if wh := clone.Webhooks.MustGet()[0]; wh.GetWebhookIncludeDataOrDefault() != "basic" || wh.GetWebhookEventsOrDefault() != 8 {
		t.Fatal("webhook settings must be copied")
	}
	if !clone.IsAnonymous.MustGet() || clone.TimeZone.MustGet() != "Europe/Copenhagen" {
		t.Fatal("settings must be copied")
	}
	if !clone.StartAt.IsSpecified() || !clone.StartAt.IsNull() {
		t.Fatal("a clone must be a draft")
	}
	if clone.StartedAt != nil || clone.ID.IsSpecified() {
		t.Fatal("a clone must not carry the source id or start")
	}
}

func TestBuildProgramCloneOtherCompany(t *testing.T) {
	companyA := uuid.New()
	companyB := uuid.New()
	src, ids := cloneTestSource(&companyA)
	clone, dropped := BuildProgramClone(src, *vo.NewString64Must("copy"), &companyB)
	if len(clone.Steps) != 1 || clone.Steps[0].CampaignTemplateID.MustGet() != ids["globalTpl"] {
		t.Fatalf("only the global template may stay, got %d steps", len(clone.Steps))
	}
	if len(clone.RecipientGroupIDs.MustGet()) != 0 {
		t.Fatal("groups must never move to another company")
	}
	if clone.EvasionPageID.IsNull() == false {
		t.Fatal("company evasion page must be dropped")
	}
	if clone.DenyPageID.MustGet() != ids["denyGlobal"] {
		t.Fatal("global deny page must stay")
	}
	if got := clone.AllowDenyIDs.MustGet(); len(got) != 1 || *got[0] != ids["filterGlobal"] {
		t.Fatal("only the global filter may stay")
	}
	if got := clone.Webhooks.MustGet(); len(got) != 1 || got[0].WebhookID.MustGet() != ids["hookGlobal"] {
		t.Fatal("only the global webhook may stay")
	}
	want := []string{"allowDeny:fc", "evasionPage:evasion", "group:group", "template:company", "template:training", "webhook:hc"}
	got := droppedKinds(dropped)
	if len(got) != len(want) {
		t.Fatalf("dropped %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dropped %v want %v", got, want)
		}
	}
}

func TestBuildProgramCloneDropsFiltersWithoutDenyPage(t *testing.T) {
	companyA := uuid.New()
	companyB := uuid.New()
	src, ids := cloneTestSource(&companyA)
	// the deny page is owned by the source company, so it can not move
	src.Pages[ids["denyGlobal"]] = programCloneItem{Name: "deny", Company: nullable.NewNullableWithValue(companyA)}
	clone, _ := BuildProgramClone(src, *vo.NewString64Must("copy"), &companyB)
	if !clone.DenyPageID.IsNull() || !clone.EvasionPageID.IsNull() {
		t.Fatal("pages must be dropped")
	}
	if len(clone.AllowDenyIDs.MustGet()) != 0 {
		t.Fatal("filters need a deny page, they must be dropped with it")
	}
	if err := ValidateProgramFilterPages(!clone.DenyPageID.IsNull(), !clone.EvasionPageID.IsNull(), len(clone.AllowDenyIDs.MustGet())); err != nil {
		t.Fatalf("clone must satisfy the page rules: %v", err)
	}
}

func TestBuildProgramCloneToGlobal(t *testing.T) {
	companyA := uuid.New()
	src, _ := cloneTestSource(&companyA)
	clone, dropped := BuildProgramClone(src, *vo.NewString64Must("copy"), nil)
	if !clone.CompanyID.IsNull() {
		t.Fatal("a global clone has no company")
	}
	if len(clone.RecipientGroupIDs.MustGet()) != 0 || len(clone.Steps) != 1 || len(dropped) == 0 {
		t.Fatal("company items must not move into a global program")
	}
}

func TestBuildProgramCloneMissingItem(t *testing.T) {
	companyA := uuid.New()
	src, ids := cloneTestSource(&companyA)
	// a template that no longer exists has no entry and is dropped
	delete(src.Templates, ids["companyTpl"])
	clone, dropped := BuildProgramClone(src, *vo.NewString64Must("copy"), &companyA)
	if len(clone.Steps) != 2 || len(dropped) != 1 || dropped[0].Kind != "template" {
		t.Fatalf("missing template must be dropped, got %d steps %v", len(clone.Steps), droppedKinds(dropped))
	}
}

func TestValidateProgramFilterPages(t *testing.T) {
	if err := ValidateProgramFilterPages(false, true, 0); err == nil {
		t.Fatal("evasion without deny page must fail")
	}
	if err := ValidateProgramFilterPages(false, false, 1); err == nil {
		t.Fatal("filters without deny page must fail")
	}
	if err := ValidateProgramFilterPages(true, true, 2); err != nil {
		t.Fatalf("valid setup rejected: %v", err)
	}
	if err := ValidateProgramFilterPages(false, false, 0); err != nil {
		t.Fatalf("nothing set must pass: %v", err)
	}
}

func TestProgramChangedFields(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	tpl := uuid.New()
	existing := &model.Program{
		Name:              nullable.NewNullableWithValue(*vo.NewString64Must("p")),
		SpreadMinutes:     nullable.NewNullableWithValue(60),
		TimeZone:          nullable.NewNullableWithValue("UTC"),
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&a, &b}),
		AllowDenyIDs:      nullable.NewNullableWithValue([]*uuid.UUID{}),
		Steps:             []*model.ProgramStep{{CampaignTemplateID: nullable.NewNullableWithValue(tpl)}},
	}
	incoming := &model.Program{
		Name:              nullable.NewNullableWithValue(*vo.NewString64Must("p")),
		SpreadMinutes:     nullable.NewNullableWithValue(120),
		TimeZone:          nullable.NewNullableWithValue("UTC"),
		RecipientGroupIDs: nullable.NewNullableWithValue([]*uuid.UUID{&b, &a}),
		AllowDenyIDs:      nullable.NewNullableWithValue([]*uuid.UUID{&a}),
		Steps:             []*model.ProgramStep{{CampaignTemplateID: nullable.NewNullableWithValue(tpl)}},
	}
	got := ProgramChangedFields(existing, incoming)
	want := map[string]bool{"spreadMinutes": true, "allowDenyIDs": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, f := range got {
		if !want[f] {
			t.Fatalf("unexpected changed field %q in %v", f, got)
		}
	}
	incoming.Steps = []*model.ProgramStep{{CampaignTemplateID: nullable.NewNullableWithValue(uuid.New())}}
	got = ProgramChangedFields(existing, incoming)
	if got[len(got)-1] != "steps" {
		t.Fatalf("step change must be listed, got %v", got)
	}
}
