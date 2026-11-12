package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
)

// audit detail keys are camelCase with Id, Ids and Ip, never ID, snake case
// or dashes, so one search finds a key in every event
func TestAuditDetailKeyStyle(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	keyRe := regexp.MustCompile(`Details\["([^"]+)"\]`)
	good := regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)
	bad := regexp.MustCompile(`(ID|IDs|IP)$`)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range keyRe.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			if !good.MatchString(key) || bad.MatchString(key) {
				t.Errorf("%s: audit detail key %q is not camelCase with Id, Ids or Ip", path, key)
			}
		}
	}
}

func TestSystemEventsCarryActor(t *testing.T) {
	system, err := model.NewSystemSession()
	if err != nil {
		t.Fatal(err)
	}
	ae := NewAuditEvent("Campaign.HandleCloseCampaigns", system)
	if ae.Details["actor"] != "system" {
		t.Fatalf("system event should name the actor: %v", ae.Details)
	}
	user := NewAuditEvent("Campaign.Create", adminSession(uuid.New(), data.RoleSuperAdministrator))
	if _, has := user.Details["actor"]; has {
		t.Fatalf("a user event has no actor detail: %v", user.Details)
	}
}
