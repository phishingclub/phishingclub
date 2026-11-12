package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/password"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type loginAuditFixture struct {
	t        *testing.T
	ctx      context.Context
	db       *gorm.DB
	buf      *bytes.Buffer
	users    *User
	sessions *Session
	scim     *Scim
	adminID  uuid.UUID
	company  uuid.UUID
}

const (
	loginTestUsername     = "auditadmin"
	loginTestPassword     = "correct horse battery staple"
	loginTestRecoveryCode = "recovery-code-1234"
	loginTestScimToken    = "scim-token-abcdefghijklmnop"
)

func newLoginAuditFixture(t *testing.T) *loginAuditFixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "login.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatalf("pragma: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Role{},
		&database.Company{},
		&database.User{},
		&database.Session{},
		&database.CompanyScimConfig{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	roleID := uuid.New()
	if err := db.Create(&database.Role{ID: &roleID, Name: data.RoleSuperAdministrator}).Error; err != nil {
		t.Fatalf("role: %v", err)
	}
	hash, err := password.NewHasherWithDefaultValues().Hash(loginTestPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	adminID := uuid.New()
	if err := db.Create(&database.User{
		ID:               &adminID,
		Name:             "Audit Admin",
		Username:         loginTestUsername,
		Email:            "auditadmin@example.test",
		PasswordHash:     hash,
		RoleID:           &roleID,
		TOTPEnabled:      true,
		TOTPSecret:       "JBSWY3DPEHPK3PXP",
		TOTPRecoveryCode: loginTestRecoveryCode,
	}).Error; err != nil {
		t.Fatalf("user: %v", err)
	}

	companyID := uuid.New()
	tokenHash, err := bcrypt.GenerateFromPassword([]byte(loginTestScimToken), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	scimID := uuid.New()
	if err := db.Create(&database.CompanyScimConfig{
		ID:          &scimID,
		CompanyID:   &companyID,
		TokenHash:   string(tokenHash),
		TokenPrefix: loginTestScimToken[:8],
		Enabled:     true,
	}).Error; err != nil {
		t.Fatalf("scim config: %v", err)
	}

	buf := &bytes.Buffer{}
	common := Common{
		Logger: zap.NewNop().Sugar(),
		AuditLogger: zap.New(zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.AddSync(buf),
			zapcore.InfoLevel,
		)).Sugar(),
	}
	scimConfigRepo := &repository.CompanyScimConfig{DB: db}
	return &loginAuditFixture{
		t:   t,
		ctx: context.Background(),
		db:  db,
		buf: buf,
		users: &User{
			Common:           common,
			UserRepository:   &repository.User{DB: db},
			PasswordVerifier: password.NewArgon2Verifier(),
			TOTPReplayCache:  NewTOTPReplayCache(),
		},
		sessions: &Session{Common: common, SessionRepository: &repository.Session{DB: db}},
		scim: &Scim{
			Common:                      common,
			CompanyScimConfigRepository: scimConfigRepo,
			CompanyScimConfigService: &CompanyScimConfig{
				Common:                      common,
				CompanyScimConfigRepository: scimConfigRepo,
			},
		},
		adminID: adminID,
		company: companyID,
	}
}

// events returns the decoded audit lines with the given event name
func (f *loginAuditFixture) events(name string) []map[string]any {
	f.t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(f.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			f.t.Fatalf("audit line is not JSON: %q", line)
		}
		if m["name"] == name {
			out = append(out, m)
		}
	}
	return out
}

func details(e map[string]any) map[string]any {
	d, _ := e["details"].(map[string]any)
	return d
}

// insertSession stores an active session for the user and returns its id
func (f *loginAuditFixture) insertSession(userID uuid.UUID, ip string) *uuid.UUID {
	f.t.Helper()
	now := time.Now()
	expires := now.Add(time.Hour)
	id, err := f.sessions.SessionRepository.Insert(f.ctx, &model.Session{
		ExpiresAt: &expires,
		MaxAgeAt:  &expires,
		IP:        ip,
		User:      &model.User{ID: nullable.NewNullableWithValue(userID)},
	})
	if err != nil {
		f.t.Fatalf("insert session: %v", err)
	}
	return id
}

func adminSession(id uuid.UUID, roleName string) *model.Session {
	return &model.Session{
		IP: "198.51.100.1",
		User: &model.User{
			ID:   nullable.NewNullableWithValue(id),
			Role: &model.Role{Name: roleName},
		},
	}
}

func TestLoginPasswordAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	ip := "203.0.113.5"

	if _, err := f.users.AuthenticateUsernameWithPassword(f.ctx, "bad user!", "whatever whatever 1", ip); err == nil {
		t.Fatal("expected invalid username to fail")
	}
	if _, err := f.users.AuthenticateUsernameWithPassword(f.ctx, loginTestUsername, "short1", ip); err == nil {
		t.Fatal("expected short password to fail")
	}
	if _, err := f.users.AuthenticateUsernameWithPassword(f.ctx, "nosuchuser", "whatever whatever 1", ip); err == nil {
		t.Fatal("expected unknown user to fail")
	}
	if _, err := f.users.AuthenticateUsernameWithPassword(f.ctx, loginTestUsername, "wrong password 1", ip); err == nil {
		t.Fatal("expected wrong password to fail")
	}
	if _, err := f.users.AuthenticateUsernameWithPassword(f.ctx, loginTestUsername, loginTestPassword, ip); err != nil {
		t.Fatalf("expected login to pass: %v", err)
	}

	ev := f.events("User.LoginPassword")
	if len(ev) != 5 {
		t.Fatalf("expected 5 password events, got %d: %v", len(ev), ev)
	}
	want := []struct {
		authorized bool
		reason     string
		username   string
		userID     string
	}{
		{false, "invalidUsername", "", ""},
		{false, "invalidPassword", loginTestUsername, ""},
		{false, "unknownUser", "nosuchuser", ""},
		{false, "wrongPassword", loginTestUsername, ""},
		{true, "", loginTestUsername, f.adminID.String()},
	}
	for i, w := range want {
		e := ev[i]
		if e["authorized"] != w.authorized || e["ip"] != ip || e["userId"] != w.userID {
			t.Errorf("event %d: %v", i, e)
		}
		d := details(e)
		if got, _ := d["username"].(string); got != w.username {
			t.Errorf("event %d username: %v", i, d)
		}
		if w.reason != "" && d["reason"] != w.reason {
			t.Errorf("event %d reason: %v", i, d)
		}
		if _, hasPassword := d["password"]; hasPassword {
			t.Errorf("event %d must not hold a password", i)
		}
	}
	if strings.Contains(f.buf.String(), loginTestPassword) || strings.Contains(f.buf.String(), "wrong password 1") ||
		strings.Contains(f.buf.String(), "short1") || strings.Contains(f.buf.String(), "bad user!") {
		t.Fatal("a password reached the audit log")
	}
	if len(f.events("User.AuthenticateUsernameWithPassword")) != 0 {
		t.Fatal("old event name is still written")
	}
}

func TestLoginMFAAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	ip := "203.0.113.6"

	// letters can never match a numeric TOTP
	if err := f.users.CheckLoginTOTP(f.ctx, &f.adminID, vo.NewString64Must("abcdef"), ip); err == nil {
		t.Fatal("expected wrong TOTP to fail")
	}
	if _, err := f.users.CheckLoginMFARecoveryCode(f.ctx, &f.adminID, vo.NewString64Must("not-the-code"), ip); err == nil {
		t.Fatal("expected wrong recovery code to fail")
	}
	ok, err := f.users.CheckLoginMFARecoveryCode(f.ctx, &f.adminID, vo.NewString64Must(loginTestRecoveryCode), ip)
	if err != nil || !ok {
		t.Fatalf("expected recovery code to pass: %v", err)
	}
	if err := f.users.DisableTOTP(f.ctx, nil, &f.adminID, "203.0.113.6"); err != nil {
		t.Fatal(err)
	}

	ev := f.events("User.LoginMFA")
	if len(ev) != 3 {
		t.Fatalf("expected 3 MFA events, got %d: %v", len(ev), ev)
	}
	want := []struct {
		authorized bool
		method     string
		reason     string
	}{
		{false, "totp", "wrongCode"},
		{false, "recoveryCode", "wrongCode"},
		{true, "recoveryCode", ""},
	}
	for i, w := range want {
		e := ev[i]
		d := details(e)
		if e["authorized"] != w.authorized || e["ip"] != ip || e["userId"] != f.adminID.String() ||
			d["method"] != w.method || (w.reason != "" && d["reason"] != w.reason) {
			t.Errorf("event %d: %v", i, e)
		}
	}
	if strings.Contains(f.buf.String(), loginTestRecoveryCode) || strings.Contains(f.buf.String(), "not-the-code") {
		t.Fatal("a recovery code reached the audit log")
	}

	disable := f.events("User.DisableTOTP")
	if len(disable) != 1 || disable[0]["userId"] != f.adminID.String() || disable[0]["ip"] != "203.0.113.6" ||
		details(disable[0])["reason"] != "recoveryCodeUsed" {
		t.Fatalf("disable event should name the user and the reason: %v", disable)
	}
}

func TestSSOUserLookupAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	ip := "203.0.113.7"

	if _, err := f.users.CreateFromSSO(f.ctx, "Nobody", "nobody@example.test", "sub-1", ip); err == nil {
		t.Fatal("expected an unprovisioned user to be refused")
	}
	id, err := f.users.CreateFromSSO(f.ctx, "Audit Admin", "auditadmin@example.test", "sub-2", ip)
	if err != nil || id == nil {
		t.Fatalf("expected the provisioned user to pass: %v", err)
	}

	ev := f.events("User.SSOLogin")
	if len(ev) != 2 {
		t.Fatalf("expected 2 SSO events, got %d: %v", len(ev), ev)
	}
	if ev[0]["authorized"] != false || ev[0]["ip"] != ip || details(ev[0])["reason"] != "notProvisioned" {
		t.Errorf("refused SSO event: %v", ev[0])
	}
	if ev[1]["authorized"] != true || ev[1]["ip"] != ip || ev[1]["userId"] != f.adminID.String() {
		t.Errorf("accepted SSO event: %v", ev[1])
	}
}

func TestSSOCallbackFailureAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	s := &SSO{Common: f.users.Common}
	s.auditSSOFailure(data.SSOProviderOIDC, "203.0.113.8", "exchangeFailed")
	ev := f.events("User.SSOLogin")
	if len(ev) != 1 || ev[0]["authorized"] != false || ev[0]["ip"] != "203.0.113.8" ||
		details(ev[0])["provider"] != data.SSOProviderOIDC || details(ev[0])["reason"] != "exchangeFailed" {
		t.Fatalf("callback failure event: %v", ev)
	}
}

func TestLogoutAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	sessionID := f.insertSession(f.adminID, "203.0.113.9")

	if err := f.sessions.Logout(f.ctx, sessionID, "203.0.113.10"); err != nil {
		t.Fatal(err)
	}
	// the session is gone, a second logout writes no event
	if err := f.sessions.Logout(f.ctx, sessionID, "203.0.113.10"); err != nil {
		t.Fatal(err)
	}
	random := uuid.New()
	if err := f.sessions.Logout(f.ctx, &random, "203.0.113.10"); err != nil {
		t.Fatal(err)
	}

	ev := f.events("User.Logout")
	if len(ev) != 1 {
		t.Fatalf("expected one logout event, got %d: %v", len(ev), ev)
	}
	if ev[0]["authorized"] != true || ev[0]["userId"] != f.adminID.String() || ev[0]["ip"] != "203.0.113.10" ||
		details(ev[0])["sessionIdPrefix"] != sessionID.String()[:8] {
		t.Fatalf("logout event: %v", ev[0])
	}
	if _, err := f.sessions.SessionRepository.GetByID(f.ctx, sessionID, &repository.SessionOption{}); err == nil {
		t.Fatal("session should be expired")
	}
}

func TestExpireSessionByIDAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	target := f.insertSession(f.adminID, "203.0.113.11")
	actorID := uuid.New()

	err := f.sessions.ExpireByID(f.ctx, adminSession(actorID, data.RoleCompanyUser), target)
	if err == nil {
		t.Fatal("expected a role without permissions to be refused")
	}
	if _, err := f.sessions.SessionRepository.GetByID(f.ctx, target, &repository.SessionOption{}); err != nil {
		t.Fatal("a refused expire must leave the session active")
	}
	if err := f.sessions.ExpireByID(f.ctx, adminSession(actorID, data.RoleSuperAdministrator), target); err != nil {
		t.Fatal(err)
	}

	ev := f.events("Session.Expire")
	if len(ev) != 2 {
		t.Fatalf("expected 2 expire events, got %d: %v", len(ev), ev)
	}
	if ev[0]["authorized"] != false || ev[0]["userId"] != actorID.String() {
		t.Errorf("refused expire event: %v", ev[0])
	}
	d := details(ev[1])
	if ev[1]["authorized"] != true || ev[1]["userId"] != actorID.String() ||
		d["sessionIdPrefix"] != target.String()[:8] || d["userId"] != f.adminID.String() {
		t.Errorf("expire event: %v", ev[1])
	}
}

func TestScimAuthFailedAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	ip := "203.0.113.12"

	if _, ok, err := f.scim.VerifyAndLoadConfig(f.ctx, &f.company, "wrong-token", ip); err != nil || ok {
		t.Fatalf("expected a refused token, got ok=%v err=%v", ok, err)
	}
	unknown := uuid.New()
	if _, ok, _ := f.scim.VerifyAndLoadConfig(f.ctx, &unknown, "wrong-token", ip); ok {
		t.Fatal("unknown company must not authenticate")
	}
	if _, ok, err := f.scim.VerifyAndLoadConfig(f.ctx, &f.company, loginTestScimToken, ip); err != nil || !ok {
		t.Fatalf("expected the right token to pass: %v", err)
	}

	ev := f.events("Scim.AuthFailed")
	if len(ev) != 1 {
		t.Fatalf("expected one event for the configured company only, got %d: %v", len(ev), ev)
	}
	d := details(ev[0])
	if ev[0]["authorized"] != false || ev[0]["ip"] != ip || d["companyId"] != f.company.String() || d["reason"] != "wrongToken" {
		t.Fatalf("scim auth event: %v", ev[0])
	}
	if strings.Contains(f.buf.String(), "wrong-token") || strings.Contains(f.buf.String(), loginTestScimToken) {
		t.Fatal("a bearer token reached the audit log")
	}
}

func TestLoginPasswordNoPasswordAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	if err := f.db.Model(&database.User{}).Where("id = ?", f.adminID.String()).
		Update("password_hash", "").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.AuthenticateUsernameWithPassword(f.ctx, loginTestUsername, loginTestPassword, "203.0.113.20"); err == nil {
		t.Fatal("expected a user without a password to be refused")
	}
	ev := f.events("User.LoginPassword")
	if len(ev) != 1 || ev[0]["authorized"] != false || details(ev[0])["reason"] != "noPassword" {
		t.Fatalf("no password event: %v", ev)
	}
}

func TestRejectLoginMFAAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	f.users.RejectLoginMFA(&f.adminID, "203.0.113.21", "bothCodes")
	ev := f.events("User.LoginMFA")
	if len(ev) != 1 || ev[0]["authorized"] != false || ev[0]["userId"] != f.adminID.String() ||
		ev[0]["ip"] != "203.0.113.21" || details(ev[0])["reason"] != "bothCodes" {
		t.Fatalf("rejected MFA event: %v", ev)
	}
}

func TestSSOInvalidEmailAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	if _, err := f.users.CreateFromSSO(f.ctx, "X", "not an email", "sub-3", "203.0.113.22"); err == nil {
		t.Fatal("expected an invalid email to be refused")
	}
	ev := f.events("User.SSOLogin")
	if len(ev) != 1 || ev[0]["authorized"] != false || details(ev[0])["reason"] != "invalidEmail" {
		t.Fatalf("invalid email event: %v", ev)
	}
	if _, has := details(ev[0])["email"]; has {
		t.Fatal("an invalid email value must not be logged")
	}
}

func TestOIDCFailureReason(t *testing.T) {
	cases := map[string]error{
		"emailNotVerified": errs.Wrap(errs.ErrSSOEmailNotVerified),
		"noEmail":          errs.Wrap(errs.ErrSSONoEmail),
		"mfaRequired":      fmt.Errorf("%w (acr)", errs.ErrSSOMFARequired),
		"exchangeFailed":   errors.New("oauth2: invalid_grant"),
	}
	for want, err := range cases {
		if got := oidcFailureReason(err); got != want {
			t.Errorf("oidcFailureReason(%v) = %s, want %s", err, got, want)
		}
	}
}

func TestExpireSessionByIDUnknownAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	unknown := uuid.New()
	if err := f.sessions.ExpireByID(f.ctx, adminSession(uuid.New(), data.RoleSuperAdministrator), &unknown); err != nil {
		t.Fatal(err)
	}
	ev := f.events("Session.Expire")
	if len(ev) != 1 || ev[0]["authorized"] != true || details(ev[0])["sessionIdPrefix"] != unknown.String()[:8] {
		t.Fatalf("expire unknown event: %v", ev)
	}
	if _, has := details(ev[0])["userId"]; has {
		t.Fatal("an unknown session has no owner to name")
	}
}

func TestScimAuthDisabledAudit(t *testing.T) {
	f := newLoginAuditFixture(t)
	if err := f.db.Model(&database.CompanyScimConfig{}).Where("company_id = ?", f.company.String()).
		Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if _, ok, err := f.scim.VerifyAndLoadConfig(f.ctx, &f.company, loginTestScimToken, "203.0.113.23"); err != nil || ok {
		t.Fatalf("expected a disabled config to refuse, ok=%v err=%v", ok, err)
	}
	ev := f.events("Scim.AuthFailed")
	if len(ev) != 1 || details(ev[0])["reason"] != "disabled" {
		t.Fatalf("disabled scim event: %v", ev)
	}
}

// the full session id is the login cookie and is never in the audit log
func TestSessionIDNotInAuditLog(t *testing.T) {
	f := newLoginAuditFixture(t)
	a := f.insertSession(f.adminID, "203.0.113.40")
	b := f.insertSession(f.adminID, "203.0.113.41")
	if err := f.sessions.Logout(f.ctx, a, "203.0.113.40"); err != nil {
		t.Fatal(err)
	}
	if err := f.sessions.ExpireByID(f.ctx, adminSession(uuid.New(), data.RoleSuperAdministrator), b); err != nil {
		t.Fatal(err)
	}
	out := f.buf.String()
	if strings.Contains(out, a.String()) || strings.Contains(out, b.String()) {
		t.Fatalf("a full session id reached the audit log: %q", out)
	}
}

// refused SCIM tokens write at most one event per company per interval
func TestScimAuthFailedThrottle(t *testing.T) {
	f := newLoginAuditFixture(t)
	for i := 0; i < 3; i++ {
		_, _, _ = f.scim.VerifyAndLoadConfig(f.ctx, &f.company, "wrong-token", "203.0.113.42")
	}
	if ev := f.events("Scim.AuthFailed"); len(ev) != 1 {
		t.Fatalf("expected one event for three refusals, got %d", len(ev))
	}
	// move the last event back past the interval
	scimAuthFailedThrottle.mu.Lock()
	scimAuthFailedThrottle.byKey[f.company.String()].last = time.Now().Add(-2 * scimAuthFailedThrottle.interval)
	scimAuthFailedThrottle.mu.Unlock()
	_, _, _ = f.scim.VerifyAndLoadConfig(f.ctx, &f.company, "wrong-token", "203.0.113.42")
	ev := f.events("Scim.AuthFailed")
	if len(ev) != 2 || details(ev[1])["suppressed"] != float64(2) {
		t.Fatalf("second event should count the two suppressed refusals: %v", ev)
	}
}

func TestAuditURLDropsUserInfo(t *testing.T) {
	got := auditURL("https://user:hunter2@box.example.test/path")
	if strings.Contains(got, "hunter2") || strings.Contains(got, "user") {
		t.Fatalf("user info must not be logged: %s", got)
	}
	if got != "https://box.example.test/path" {
		t.Fatalf("unexpected url: %s", got)
	}
}

func TestAPIKeyFailedThrottleAndActor(t *testing.T) {
	f := newLoginAuditFixture(t)
	apiKeyFailedThrottle.mu.Lock()
	delete(apiKeyFailedThrottle.byKey, "apiKey")
	apiKeyFailedThrottle.mu.Unlock()
	f.users.AuditAPIKeyFailed("203.0.113.50")
	f.users.AuditAPIKeyFailed("203.0.113.51")
	ev := f.events("User.APIKeyFailed")
	if len(ev) != 1 || ev[0]["result"] != "denied" || ev[0]["ip"] != "203.0.113.50" {
		t.Fatalf("expected one denied event: %v", ev)
	}
	sid := uuid.MustParse(data.APISessionID)
	api := adminSession(uuid.New(), data.RoleSuperAdministrator)
	api.ID = &sid
	if NewAuditEvent("X.Y", api).Details["actor"] != "api" {
		t.Fatal("an API key session should be marked as actor api")
	}
}

func TestAuditURLMalformed(t *testing.T) {
	for _, u := range []string{"user:secret@bb.example", "https:user:secret@bb.example", "https:/user:secret@bb.example"} {
		if got := auditURL(u); strings.Contains(got, "secret") {
			t.Errorf("%q leaked as %q", u, got)
		}
	}
}
