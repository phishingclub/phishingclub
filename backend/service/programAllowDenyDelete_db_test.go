package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// TestAllowDenyDeleteRefusedForOpenProgram checks a list a program that is not
// closed filters with is not deleted, so the program never starts to show its
// pages to everyone, and that the list is no longer held once the program closed
func TestAllowDenyDeleteRefusedForOpenProgram(t *testing.T) {
	p := newProgramServiceTest(t)
	if err := p.db.AutoMigrate(&database.AllowDeny{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	programID := p.program(model.ProgramLoopComplete)
	list := uuid.New()
	now := time.Now().UTC()
	p.create(&database.AllowDeny{ID: &list, CreatedAt: &now, UpdatedAt: &now, Name: "office", Allowed: true})
	p.create(&database.ProgramAllowDeny{ProgramID: &programID, AllowDenyID: &list})
	svc := &AllowDeny{
		Common:            p.svc.Common,
		ProgramRepository: p.svc.ProgramRepository,
	}
	if err := svc.DeleteByID(p.ctx, authorizedSession(t), &list); err == nil {
		t.Fatal("a list an open program uses must not be deleted")
	}
	var n int64
	p.db.Model(&database.ProgramAllowDeny{}).Where("allow_deny_id = ?", list).Count(&n)
	if n != 1 {
		t.Fatal("the refused delete must keep the list on the program")
	}
	p.db.Model(&database.Program{}).Where("id = ?", programID).Update("closed_at", now)
	uses, err := (&repository.Program{DB: p.db}).GetOpenUsingAllowDeny(p.ctx, &list)
	if err != nil || len(uses) != 0 {
		t.Fatalf("a closed program must not hold the list: %v %v", uses, err)
	}
}
