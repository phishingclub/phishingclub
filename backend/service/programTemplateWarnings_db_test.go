package service

import (
	"strings"
	"testing"

	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// TestTemplateWarningsNameSendingProfile checks that the warning for a template
// that can not be sent names the SMTP configuration and API sender too
func TestTemplateWarningsNameSendingProfile(t *testing.T) {
	p := newProgramServiceTest(t)
	p.svc.CampaignTemplateService = &CampaignTemplate{
		Common:                     p.svc.Common,
		CampaignTemplateRepository: &repository.CampaignTemplate{DB: p.db},
	}
	programID := p.program(model.ProgramLoopComplete)
	p.step(programID, 0, false)
	p.step(programID, 1, true)

	warnings := p.svc.templateWarnings(p.ctx, authorizedSession(t), p.loadProgram(programID))
	if len(warnings) != 1 {
		t.Fatalf("want 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "missing a domain, email, page, or an SMTP configuration or API sender") {
		t.Fatalf("warning does not name the sending profile: %q", warnings[0])
	}
}
