package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap/zapcore"
)

// TestProgramCloneLanguageRouting checks a copy in the same company keeps the
// field, the default and the routes, and a copy to another company keeps the
// field and the default, drops the routes, lists them as left out and audits
// how many were dropped
func TestProgramCloneLanguageRouting(t *testing.T) {
	f := newDeleteFixture(t)
	logger, buf := bufferLogger(zapcore.InfoLevel)
	f.svc.AuditLogger = logger
	session := authorizedSession(t)
	withProgramLanguageServices(f.programTest)
	programID := f.program(model.ProgramLoopComplete)
	f.templateStep(programID, f.languageTemplate("clone lure", "en", "de", "da"), 0)
	if err := f.db.Model(&database.Program{}).Where("id = ?", programID).Updates(map[string]any{
		"language_route_by": "country", "language_default": "en",
	}).Error; err != nil {
		t.Fatal(err)
	}
	for key, code := range map[string]string{"germany": "de", "denmark": "da"} {
		f.create(&database.ProgramLanguageRoute{ProgramID: &programID, MatchKey: key, Language: code})
	}
	stored := func(id *uuid.UUID) *model.Program {
		t.Helper()
		p, err := f.svc.ProgramRepository.GetByID(f.ctx, id, &repository.ProgramOption{WithLanguageRoutes: true})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	same, err := f.svc.Clone(f.ctx, session, &programID, *vo.NewString64Must("same copy"), nil)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	copied := stored(same.ID)
	routes, _ := copied.LanguageRoutes.Get()
	if copied.LanguageRouteField() != "country" || copied.LanguageDefault.MustGet() != "en" ||
		len(routes) != 2 || routes["germany"] != "de" || routes["denmark"] != "da" {
		t.Fatalf("a same company copy keeps the routing, got %q %q %v",
			copied.LanguageRouteField(), copied.LanguageDefault.MustGet(), routes)
	}
	for _, d := range same.Dropped {
		if d.Kind == "languageRoutes" {
			t.Fatal("a same company copy drops no routes")
		}
	}

	companyID := f.company("Other")
	other, err := f.svc.Clone(f.ctx, session, &programID, *vo.NewString64Must("other copy"), &companyID)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	moved := stored(other.ID)
	routes, _ = moved.LanguageRoutes.Get()
	if moved.LanguageRouteField() != "country" || moved.LanguageDefault.MustGet() != "en" || len(routes) != 0 {
		t.Fatalf("another company keeps the field and default only, got %q %q %v",
			moved.LanguageRouteField(), moved.LanguageDefault.MustGet(), routes)
	}
	listed := false
	for _, d := range other.Dropped {
		if d.Kind == "languageRoutes" && d.Name == "2 values" {
			listed = true
		}
	}
	if !listed {
		t.Fatalf("the dropped routes must be listed, got %v", droppedKinds(other.Dropped))
	}
	events := auditLines(t, buf)["Program.Clone"]
	if len(events) != 2 {
		t.Fatalf("expected two clone events, got %d", len(events))
	}
	first, _ := events[0]["details"].(map[string]any)
	second, _ := events[1]["details"].(map[string]any)
	if _, ok := first["droppedLanguageRoutes"]; ok {
		t.Fatalf("a same company copy audits no dropped routes: %v", first)
	}
	if second["droppedLanguageRoutes"] != float64(2) {
		t.Fatalf("dropped routes in audit: %v", second)
	}
}

// TestProgramCloneAndStartCheckLanguages checks a copy in the same company and
// a start run the language check: a routed language a step template can no
// longer send is refused, naming the template and the language
func TestProgramCloneAndStartCheckLanguages(t *testing.T) {
	f := newDeleteFixture(t)
	withProgramLanguageServices(f.programTest)
	session := authorizedSession(t)
	templateID := f.languageTemplate("start lure", "en", "de")
	programID := f.program(model.ProgramLoopComplete)
	f.templateStep(programID, templateID, 0)
	f.routeProgram(programID, "country", "en", map[string]string{"Germany": "de"})
	// a draft with a group, so only the language check can refuse the start
	f.setProgram(programID, map[string]any{"started_at": nil, "start_at": nil})
	groupID := uuid.New()
	now := time.Now().UTC()
	f.create(&database.RecipientGroup{ID: &groupID, CreatedAt: &now, UpdatedAt: &now, Name: "start group"})
	f.create(&database.ProgramRecipientGroup{ProgramID: &programID, RecipientGroupID: &groupID})
	// the German row stops being usable after the program was saved
	if err := f.db.Model(&database.CampaignTemplateLanguage{}).Where("campaign_template_id = ? AND language = ?", templateID, "de").
		Update("is_usable", false).Error; err != nil {
		t.Fatal(err)
	}
	refused := func(what string, err error) {
		t.Helper()
		if !isValidationError(err) || !strings.Contains(err.Error(), "start lure") || !strings.Contains(err.Error(), "German") {
			t.Fatalf("%s: want a validation error naming the template and German, got %v", what, err)
		}
	}
	_, err := f.svc.Clone(f.ctx, session, &programID, *vo.NewString64Must("broken copy"), nil)
	refused("same company copy", err)
	refused("start", f.svc.Start(f.ctx, session, &programID))
	if p := f.loadProgram(programID); p.StartAt.IsSpecified() && !p.StartAt.IsNull() {
		t.Fatal("a refused start must not set a start time")
	}
	// once the row is usable again both work
	if err := f.db.Model(&database.CampaignTemplateLanguage{}).Where("campaign_template_id = ?", templateID).
		Update("is_usable", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Clone(f.ctx, session, &programID, *vo.NewString64Must("good copy"), nil); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if err := f.svc.startWait(f.ctx, session, &programID); err != nil {
		t.Fatalf("start: %v", err)
	}
}
