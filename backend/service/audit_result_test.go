package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/vo"
)

func TestAuditDoneWritesFailedOnce(t *testing.T) {
	logger, buf := bufferLogger(0)
	c := Common{Logger: logger}

	const secret = "value-that-must-not-be-logged"
	run := func() (err error) {
		ae := NewAuditEvent("Thing.Create", nil)
		defer c.auditDone(ae, &err)
		return errs.NewValidationError(errors.New("bad " + secret))
	}
	_ = run()

	out := buf.String()
	if strings.Count(out, `"name":"Thing.Create"`) != 1 {
		t.Fatalf("expected one event, got %q", out)
	}
	for _, want := range []string{`"result":"failed"`, `"authorized":true`, `"errorClass":"validation"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %q", want, out)
		}
	}
	if strings.Contains(out, secret) {
		t.Fatal("the error text reached the audit log")
	}
}

// auditLines decodes the audit lines in buf by event name
func auditLines(t *testing.T, buf *bytes.Buffer) map[string][]map[string]any {
	t.Helper()
	out := map[string][]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not JSON: %q", line)
		}
		name, _ := m["name"].(string)
		out[name] = append(out[name], m)
	}
	return out
}

func TestAuditDoneSkipsWrittenAndNil(t *testing.T) {
	logger, buf := bufferLogger(0)
	c := Common{Logger: logger}

	// success already written, a later error is not a second event
	func() (err error) {
		ae := NewAuditEvent("A.Success", nil)
		defer c.auditDone(ae, &err)
		c.AuditLogAuthorized(ae)
		return errors.New("after success")
	}()
	// denied already written
	func() (err error) {
		ae := NewAuditEvent("A.Denied", nil)
		defer c.auditDone(ae, &err)
		c.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}()
	// no error and nothing written, no event
	func() (err error) {
		ae := NewAuditEvent("A.Nil", nil)
		defer c.auditDone(ae, &err)
		return nil
	}()

	ev := auditLines(t, buf)
	if len(ev["A.Success"]) != 1 || ev["A.Success"][0]["result"] != "success" {
		t.Fatalf("success event: %v", ev["A.Success"])
	}
	if len(ev["A.Denied"]) != 1 || ev["A.Denied"][0]["result"] != "denied" {
		t.Fatalf("denied event: %v", ev["A.Denied"])
	}
	if len(ev["A.Nil"]) != 0 {
		t.Fatalf("nil error without a written event must write nothing: %v", ev["A.Nil"])
	}
}

func TestAuditFailedClasses(t *testing.T) {
	logger, buf := bufferLogger(0)
	c := Common{Logger: logger}
	fail := func(name string, failure error) {
		func() (err error) {
			ae := NewAuditEvent(name, nil)
			defer c.auditDone(ae, &err)
			return failure
		}()
	}
	fail("A.WrongTOTP", errs.Wrap(errs.ErrUserWrongTOTP))
	fail("A.WrongPassword", errs.ErrAuthenticationFailed)
	fail("A.Paused", errs.Wrap(errs.ErrCampaignAlreadyPaused))
	fail("A.Closed", errs.ErrCampaignAlreadyClosed)

	ev := auditLines(t, buf)
	for _, name := range []string{"A.WrongTOTP", "A.WrongPassword"} {
		e := ev[name]
		if len(e) != 1 || e[0]["result"] != "denied" || e[0]["authorized"] != false {
			t.Errorf("%s should be denied: %v", name, e)
		}
	}
	for _, name := range []string{"A.Paused", "A.Closed"} {
		e := ev[name]
		d, _ := e[0]["details"].(map[string]any)
		if len(e) != 1 || e[0]["result"] != "failed" || d["errorClass"] != "state" {
			t.Errorf("%s should be a failed state event: %v", name, e)
		}
	}
}

func TestAuditDoneSkipsSystemFailures(t *testing.T) {
	logger, buf := bufferLogger(0)
	c := Common{Logger: logger}
	system, err := model.NewSystemSession()
	if err != nil {
		t.Fatal(err)
	}
	func() (err error) {
		ae := NewAuditEvent("A.Timer", system)
		defer c.auditDone(ae, &err)
		return errors.New("db locked")
	}()
	// an explicit success of a system action is still written
	func() (err error) {
		ae := NewAuditEvent("A.TimerDone", system)
		defer c.auditDone(ae, &err)
		c.AuditLogAuthorized(ae)
		return nil
	}()
	ev := auditLines(t, buf)
	if len(ev["A.Timer"]) != 0 {
		t.Fatalf("system failures must not be written by auditDone: %v", ev["A.Timer"])
	}
	if len(ev["A.TimerDone"]) != 1 {
		t.Fatalf("system success should be written: %v", ev)
	}
}

func TestAuditDoneNestedAuthorizationIsDenied(t *testing.T) {
	logger, buf := bufferLogger(0)
	c := Common{Logger: logger}
	func() (err error) {
		ae := NewAuditEvent("A.Nested", nil)
		defer c.auditDone(ae, &err)
		return errs.Wrap(errs.ErrAuthorizationFailed)
	}()
	out := buf.String()
	if !strings.Contains(out, `"result":"denied"`) || !strings.Contains(out, `"authorized":false`) {
		t.Fatalf("nested authorization error should be denied: %q", out)
	}
}

// a converted service function: an invalid option key is a validation
// failure after the permission check
func TestSetOptionByKeyFailedIsAudited(t *testing.T) {
	logger, buf := bufferLogger(0)
	o := &Option{Common: Common{Logger: logger}}
	session := adminSession(uuid.New(), data.RoleSuperAdministrator)
	err := o.SetOptionByKey(context.Background(), session, &model.Option{
		Key:   *vo.NewString127Must("no_such_option"),
		Value: *vo.NewOptionalString1MBMust("x"),
	})
	if err == nil {
		t.Fatal("expected an invalid key to fail")
	}
	out := buf.String()
	if strings.Count(out, `"name":"Option.SetOptionByKey"`) != 1 ||
		!strings.Contains(out, `"result":"failed"`) || !strings.Contains(out, `"errorClass":"validation"`) {
		t.Fatalf("failed option event: %q", out)
	}
}

// a converted service function: the database is gone after the permission
// check passed
func TestExpireByIDFailedIsAudited(t *testing.T) {
	f := newLoginAuditFixture(t)
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	id := uuid.New()
	if err := f.sessions.ExpireByID(f.ctx, adminSession(uuid.New(), data.RoleSuperAdministrator), &id); err == nil {
		t.Fatal("expected a closed database to fail")
	}
	ev := f.events("Session.Expire")
	if len(ev) != 1 || ev[0]["result"] != "failed" || details(ev[0])["errorClass"] != "internal" {
		t.Fatalf("failed expire event: %v", ev)
	}
}

// a question back to the user writes no event
func TestAuditSkipWritesNothing(t *testing.T) {
	logger, buf := bufferLogger(0)
	c := Common{Logger: logger}
	func() (err error) {
		ae := NewAuditEvent("A.Prompt", nil)
		defer c.auditDone(ae, &err)
		ae.skip()
		return errors.New("lure code is already in use")
	}()
	if buf.Len() != 0 {
		t.Fatalf("a skipped event must write nothing: %q", buf.String())
	}
}

// the value of an unknown option key is not written, it could be a secret
func TestSetOptionByKeyUnknownKeyHidesValue(t *testing.T) {
	logger, buf := bufferLogger(0)
	o := &Option{Common: Common{Logger: logger}}
	session := adminSession(uuid.New(), data.RoleSuperAdministrator)
	_ = o.SetOptionByKey(context.Background(), session, &model.Option{
		Key:   *vo.NewString127Must("no_such_option"),
		Value: *vo.NewOptionalString1MBMust("pasted-secret-value"),
	})
	if strings.Contains(buf.String(), "pasted-secret-value") {
		t.Fatalf("the value of an unknown key reached the audit log: %q", buf.String())
	}
}
