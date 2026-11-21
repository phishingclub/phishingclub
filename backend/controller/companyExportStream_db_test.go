package controller

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// exportFixture is a company controller on a temporary database
type exportFixture struct {
	db         *gorm.DB
	controller *Company
	recipients *repository.Recipient
	companyID  uuid.UUID
}

// newExportFixture opens a temporary database with the tables the company
// export reads and wires a company controller to it
func newExportFixture(t *testing.T) *exportFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	t.Setenv("TMPDIR", t.TempDir())
	dsn := filepath.Join(t.TempDir(), "export.db") + "?_foreign_keys=OFF&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&database.Company{},
		&database.Recipient{},
		&database.RecipientGroup{},
		&database.RecipientGroupRecipient{},
		&database.CampaignTemplate{},
		&database.Campaign{},
		&database.CampaignTemplatePool{},
		&database.CampaignRecipient{},
		&database.Event{},
		&database.CampaignEvent{},
		&database.Program{},
		&database.ProgramStep{},
		&database.ProgramCampaign{},
		&database.ProgramRecipientGroup{},
		&database.ProgramEnrollment{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	common := service.Common{Logger: zap.NewNop().Sugar()}
	recipients := &repository.Recipient{DB: db}
	campaigns := &repository.Campaign{DB: db}
	campaignService := &service.Campaign{Common: common, CampaignRepository: campaigns}
	c := &Company{
		Common: Common{Response: api.NewJSONResponseHandler(), Logger: zap.NewNop().Sugar()},
		CompanyService: &service.Company{
			Common:            common,
			CompanyRepository: &repository.Company{DB: db},
			CampaignService:   campaignService,
		},
		CampaignService:  campaignService,
		RecipientService: &service.Recipient{Common: common, RecipientRepository: recipients},
	}
	companyID := uuid.New()
	at := exportTime(0)
	if err := db.Create(&database.Company{ID: companyID, CreatedAt: &at, UpdatedAt: &at, Name: "=Acme"}).Error; err != nil {
		t.Fatalf("create company: %v", err)
	}
	return &exportFixture{db: db, controller: c, recipients: recipients, companyID: companyID}
}

// exportTime is a fixed time plus minutes
func exportTime(minutes int) time.Time {
	return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
}

func (f *exportFixture) recipient(t *testing.T, companyID *uuid.UUID, first, last, email string) *uuid.UUID {
	t.Helper()
	id := uuid.New()
	at := exportTime(1)
	row := &database.Recipient{
		ID: &id, CreatedAt: &at, UpdatedAt: &at, CompanyID: companyID,
		FirstName: first, LastName: last, Position: "@pos", Department: "dept", City: " -city", Misc: "+misc",
	}
	if email != "" {
		row.Email = &email
	}
	phone := "-555"
	row.Phone = &phone
	if err := f.db.Create(row).Error; err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	return &id
}

func (f *exportFixture) group(t *testing.T, companyID *uuid.UUID, name string, members ...*uuid.UUID) {
	t.Helper()
	id := uuid.New()
	at := exportTime(0)
	if err := f.db.Create(&database.RecipientGroup{ID: &id, CreatedAt: &at, UpdatedAt: &at, Name: name, CompanyID: companyID}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, m := range members {
		if err := f.db.Create(&database.RecipientGroupRecipient{RecipientID: m, RecipientGroupID: &id}).Error; err != nil {
			t.Fatalf("add member: %v", err)
		}
	}
}

func (f *exportFixture) campaign(t *testing.T, companyID *uuid.UUID, name string, anonymous bool) *uuid.UUID {
	t.Helper()
	id := uuid.New()
	row := map[string]any{
		"id": id.String(), "created_at": utils.RFC3339UTC(exportTime(0)), "updated_at": utils.RFC3339UTC(exportTime(0)),
		"name": name, "is_anonymous": anonymous,
	}
	if companyID != nil {
		row["company_id"] = companyID.String()
	}
	if err := f.db.Model(&database.Campaign{}).Create(row).Error; err != nil {
		t.Fatalf("create campaign: %v", err)
	}
	return &id
}

func (f *exportFixture) event(t *testing.T, campaignID, recipientID, anonymizedID, eventID *uuid.UUID, minute int, data, userAgent string) {
	t.Helper()
	at := utils.RFC3339UTC(exportTime(minute))
	row := map[string]any{
		"id": uuid.New().String(), "created_at": at, "updated_at": at,
		"data": data, "campaign_id": campaignID.String(), "event_id": eventID.String(),
		"user_agent": userAgent, "ip_address": " 10.0.0.1 ",
	}
	if recipientID != nil {
		row["recipient_id"] = recipientID.String()
	}
	if anonymizedID != nil {
		row["anonymized_id"] = anonymizedID.String()
	}
	if err := f.db.Model(&database.CampaignEvent{}).Create(row).Error; err != nil {
		t.Fatalf("create event: %v", err)
	}
}

// seedExportData seeds a company and shared data with a recipient in several
// groups, formula strings, an anonymous campaign, an event of a deleted
// recipient, an event with no recipient, events that share a time and two
// campaigns with the same name
func (f *exportFixture) seedExportData(t *testing.T) {
	t.Helper()
	sent, clicked := uuid.New(), uuid.New()
	for id, name := range map[uuid.UUID]string{sent: "campaign_recipient_message_sent", clicked: "=clicked"} {
		id := id
		at := exportTime(0)
		if err := f.db.Create(&database.Event{ID: &id, CreatedAt: &at, Name: name}).Error; err != nil {
			t.Fatalf("create event type: %v", err)
		}
		cache.EventNameByID[id.String()] = name
	}
	company := &f.companyID
	alice := f.recipient(t, company, "=Alice", " +Smith ", "alice@example.com")
	bob := f.recipient(t, company, "Bob", "Builder", "bob@example.com")
	carol := f.recipient(t, company, "Carol", "-Cee", "")
	gone := f.recipient(t, company, "Gone", "Person", "gone@example.com")
	shared := f.recipient(t, nil, "@Shared", "One", "shared@example.com")
	f.group(t, company, "beta", alice, bob)
	f.group(t, company, "alpha", alice)
	f.group(t, company, "=gamma", alice, carol)
	f.group(t, nil, "shared group", shared)

	normal := f.campaign(t, company, "+Quarterly", false)
	again := f.campaign(t, company, "+quarterly", false)
	anon := f.campaign(t, company, "Anonymous", true)
	sharedCampaign := f.campaign(t, nil, "Shared campaign", false)
	for i := 0; i < 6; i++ {
		// every pair of events shares a time
		f.event(t, normal, alice, nil, &sent, i/2, "=1+1", " -agent ")
		f.event(t, normal, bob, nil, &clicked, i/2, " @data", "agent")
	}
	f.event(t, normal, gone, nil, &clicked, 5, "gone", "")
	f.event(t, normal, nil, nil, &sent, 6, "", "")
	f.event(t, again, bob, nil, &sent, 7, "again", "ua")
	for i := 0; i < 4; i++ {
		anonymized := uuid.New()
		f.event(t, anon, nil, &anonymized, &clicked, i, "anon", "ua")
	}
	f.event(t, sharedCampaign, shared, nil, &sent, 1, "shared", "ua")
	// a deleted recipient leaves its events behind
	if err := f.db.Delete(&database.Recipient{}, "id = ?", gone.String()).Error; err != nil {
		t.Fatalf("delete recipient: %v", err)
	}
}

// export runs a handler of the export and returns the response
func (f *exportFixture) export(t *testing.T, handler func(*gin.Context), companyID *uuid.UUID, sortOrder string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(w)
	target := "/export"
	if sortOrder != "" {
		target += "?sortOrder=" + sortOrder
	}
	g.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if companyID != nil {
		g.Params = gin.Params{{Key: "id", Value: companyID.String()}}
	}
	g.Set("session", exportSession())
	handler(g)
	return w
}

func exportSession() *model.Session {
	return &model.Session{
		IP: "127.0.0.1",
		User: &model.User{
			ID:   nullable.NewNullableWithValue(uuid.New()),
			Role: &model.Role{ID: uuid.New(), Name: data.RoleSuperAdministrator},
		},
	}
}

// readZip returns the entries of a zip by name, in order
func readZip(t *testing.T, body []byte) ([]string, map[string]string) {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	names := []string{}
	files := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, f.Name)
		files[f.Name] = string(b)
	}
	return names, files
}

func readCSV(t *testing.T, text string) [][]string {
	t.Helper()
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	return rows
}

// TestCompanyExportContent checks the content of the company export on the
// seeded data: the recipient in several groups gets a column per group
// sorted by name, formula strings are escaped, events of an anonymous
// campaign and of a deleted recipient are anonymized, events are newest
// first and campaigns of the same name get their own file
func TestCompanyExportContent(t *testing.T) {
	f := newExportFixture(t)
	f.seedExportData(t)
	w := f.export(t, f.controller.ExportByCompanyID, &f.companyID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	names, files := readZip(t, w.Body.Bytes())
	wantNames := []string{
		"company.csv",
		"recipients.csv",
		"campaign_events/+Quarterly.csv",
		"campaign_events/+quarterly (2).csv",
		"campaign_events/Anonymous.csv",
		"programs.csv",
	}
	if strings.Join(names, "|") != strings.Join(wantNames, "|") {
		t.Fatalf("entries %v, want %v", names, wantNames)
	}
	recipients := readCSV(t, files["recipients.csv"])
	wantHeader := "Created at|Updated at|Email|Phone|Extra Identifier|First name|Last name|Position|Department|City|Country|Misc|Group 1|Group 2|Group 3"
	if got := strings.Join(recipients[0], "|"); got != wantHeader {
		t.Fatalf("recipient header %q", got)
	}
	if len(recipients) != 4 {
		t.Fatalf("recipient rows %d, want 3 and a header", len(recipients)-1)
	}
	alice := strings.Join(recipients[1], "|")
	wantAlice := "2026-03-01T12:01:00Z|2026-03-01T12:01:00Z|alice@example.com|'-555||'=Alice|'+Smith|'@pos|dept|'-city||'+misc|'=gamma|alpha|beta"
	if alice != wantAlice {
		t.Fatalf("alice row\n%q\nwant\n%q", alice, wantAlice)
	}
	if got := recipients[3][2]; got != "" {
		t.Fatalf("recipient without email gives %q", got)
	}
	events := readCSV(t, files["campaign_events/+Quarterly.csv"])
	if len(events) != 1+14 {
		t.Fatalf("event rows %d", len(events)-1)
	}
	first := strings.Join(events[1], "|")
	if want := "'+Quarterly|2026-03-01T12:06:00Z|anonymized|anonymized|anonymized|campaign_recipient_message_sent|||10.0.0.1"; first != want {
		t.Fatalf("newest event %q, want %q", first, want)
	}
	gone := strings.Join(events[2], "|")
	if want := "'+Quarterly|2026-03-01T12:05:00Z|anonymized|anonymized|anonymized|'=clicked|gone||10.0.0.1"; gone != want {
		t.Fatalf("deleted recipient event %q, want %q", gone, want)
	}
	// of two events at the same time the later written comes first
	if got := strings.Join(events[3], "|"); got != "'+Quarterly|2026-03-01T12:02:00Z|Bob|Builder|bob@example.com|'=clicked|'@data|agent|10.0.0.1" {
		t.Fatalf("event row %q", got)
	}
	if got := strings.Join(events[4], "|"); got != "'+Quarterly|2026-03-01T12:02:00Z|'=Alice|'+Smith|alice@example.com|campaign_recipient_message_sent|'=1+1|'-agent|10.0.0.1" {
		t.Fatalf("event row %q", got)
	}
	for _, row := range readCSV(t, files["campaign_events/Anonymous.csv"])[1:] {
		if row[2] != "anonymized" || row[3] != "anonymized" || row[4] != "anonymized" {
			t.Fatalf("anonymous campaign row names a person: %v", row)
		}
	}
	// oldest first
	w = f.export(t, f.controller.ExportByCompanyID, &f.companyID, "asc")
	_, files = readZip(t, w.Body.Bytes())
	events = readCSV(t, files["campaign_events/+Quarterly.csv"])
	if got := events[1][1]; got != "2026-03-01T12:00:00Z" {
		t.Fatalf("oldest first starts at %q", got)
	}
}

// TestSharedExportContent checks the shared export holds only the shared
// recipients and campaigns
func TestSharedExportContent(t *testing.T) {
	f := newExportFixture(t)
	f.seedExportData(t)
	w := f.export(t, f.controller.ExportShared, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	names, files := readZip(t, w.Body.Bytes())
	if want := "recipients.csv|campaign_events/Shared campaign.csv|programs.csv"; strings.Join(names, "|") != want {
		t.Fatalf("entries %v", names)
	}
	recipients := readCSV(t, files["recipients.csv"])
	if len(recipients) != 2 || recipients[1][5] != "'@Shared" || recipients[1][12] != "shared group" {
		t.Fatalf("shared recipients %v", recipients)
	}
	events := readCSV(t, files["campaign_events/Shared campaign.csv"])
	if len(events) != 2 || events[1][4] != "shared@example.com" {
		t.Fatalf("shared events %v", events)
	}
}

// TestCompanyExportPageSizeSame checks the export is the same whatever the
// recipient page size, so page boundaries lose or repeat no recipient
func TestCompanyExportPageSizeSame(t *testing.T) {
	f := newExportFixture(t)
	f.seedExportData(t)
	for i := 0; i < 20; i++ {
		id := f.recipient(t, &f.companyID, fmt.Sprintf("Extra%d", i), "", fmt.Sprintf("extra%d@example.com", i))
		if i%3 == 0 {
			f.group(t, &f.companyID, fmt.Sprintf("extra group %d", i), id)
		}
	}
	want := f.export(t, f.controller.ExportByCompanyID, &f.companyID, "").Body.Bytes()
	for _, size := range []int{1, 2, 7, 23, 24, 25} {
		f.recipients.ExportPageSize = size
		got := f.export(t, f.controller.ExportByCompanyID, &f.companyID, "").Body.Bytes()
		if !bytes.Equal(got, want) {
			t.Fatalf("page size %d gives another export", size)
		}
	}
}

// TestCompanyExportManyRecipients checks a company with more recipients than
// SQLite allows variables in one statement exports every recipient with its
// groups
func TestCompanyExportManyRecipients(t *testing.T) {
	f := newExportFixture(t)
	const count = 33000
	groupA, groupB := uuid.New(), uuid.New()
	at := exportTime(0)
	for i, id := range []*uuid.UUID{&groupA, &groupB} {
		if err := f.db.Create(&database.RecipientGroup{ID: id, CreatedAt: &at, UpdatedAt: &at, Name: fmt.Sprintf("group %d", i), CompanyID: &f.companyID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows := make([]database.Recipient, 0, count)
	members := make([]database.RecipientGroupRecipient, 0, count+count/10)
	for i := 0; i < count; i++ {
		id := uuid.New()
		email := fmt.Sprintf("r%d@example.com", i)
		rows = append(rows, database.Recipient{ID: &id, CreatedAt: &at, UpdatedAt: &at, CompanyID: &f.companyID, Email: &email, FirstName: "R"})
		members = append(members, database.RecipientGroupRecipient{RecipientID: &id, RecipientGroupID: &groupA})
		if i%10 == 0 {
			members = append(members, database.RecipientGroupRecipient{RecipientID: &id, RecipientGroupID: &groupB})
		}
	}
	if err := f.db.CreateInBatches(rows, 500).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.CreateInBatches(members, 1000).Error; err != nil {
		t.Fatal(err)
	}
	w := f.export(t, f.controller.ExportByCompanyID, &f.companyID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	_, files := readZip(t, w.Body.Bytes())
	recipients := readCSV(t, files["recipients.csv"])
	if len(recipients) != count+1 {
		t.Fatalf("recipient rows %d, want %d", len(recipients)-1, count)
	}
	if len(recipients[0]) != 14 {
		t.Fatalf("header %v, want two group columns", recipients[0])
	}
	// a row has twelve fields and one per group
	two := 0
	for _, row := range recipients[1:] {
		switch len(row) {
		case 14:
			two++
		case 13:
		default:
			t.Fatalf("row %v", row)
		}
	}
	if two != count/10 {
		t.Fatalf("recipients in two groups %d, want %d", two, count/10)
	}
}

// TestCompanyExportRefusedAtEntry checks both exports refuse a session without
// the global permission before anything is read or written
func TestCompanyExportRefusedAtEntry(t *testing.T) {
	f := newExportFixture(t)
	for name, run := range map[string]func(*gin.Context){
		"company": f.controller.ExportByCompanyID,
		"shared":  f.controller.ExportShared,
	} {
		w := httptest.NewRecorder()
		g, _ := gin.CreateTestContext(w)
		g.Request = httptest.NewRequest(http.MethodGet, "/export", nil)
		g.Params = gin.Params{{Key: "id", Value: f.companyID.String()}}
		g.Set("session", &model.Session{
			IP: "127.0.0.1",
			User: &model.User{
				ID:   nullable.NewNullableWithValue(uuid.New()),
				Role: &model.Role{ID: uuid.New(), Name: data.RoleCompanyUser},
			},
		})
		run(g)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: a session without the global permission must be refused, got %d", name, w.Code)
		}
	}
}
