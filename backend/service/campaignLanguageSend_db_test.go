package service

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
)

// fakeSMTP is an SMTP server on the loopback that takes every message and
// counts the connections
type fakeSMTP struct {
	ln       net.Listener
	mu       sync.Mutex
	conns    int
	messages []string
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns++
			s.mu.Unlock()
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) port() int {
	return s.ln.Addr().(*net.TCPAddr).Port
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	r := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	write("220 fake.test ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			write("250-fake.test")
			write("250 8BITMIME")
		case strings.HasPrefix(cmd, "DATA"):
			write("354 go ahead")
			var body strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				body.WriteString(l)
			}
			s.mu.Lock()
			s.messages = append(s.messages, body.String())
			s.mu.Unlock()
			write("250 queued")
		case strings.HasPrefix(cmd, "QUIT"):
			write("221 bye")
			return
		default:
			write("250 OK")
		}
	}
}

func (s *fakeSMTP) counts() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns, append([]string{}, s.messages...)
}

// languageSendFixture is a program sender test with everything the SMTP send
// path reads, and one template written in English with a German and a French
// language row
type languageSendFixture struct {
	*programTest
	smtp     *fakeSMTP
	template uuid.UUID
	frEmail  uuid.UUID
}

func newLanguageSendFixture(t *testing.T) *languageSendFixture {
	t.Helper()
	p := newProgramSenderTest(t)
	if err := p.db.AutoMigrate(&database.SMTPHeader{}, &database.EmailAttachment{}, &database.Attachment{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, event := range []string{
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_SENT,
		data.EVENT_CAMPAIGN_RECIPIENT_MESSAGE_FAILED,
		data.EVENT_CAMPAIGN_RECIPIENT_CANCELLED,
		data.EVENT_CAMPAIGN_ACTIVE,
		data.EVENT_CAMPAIGN_CLOSED,
	} {
		if cache.EventIDByName[event] == nil {
			id := uuid.New()
			cache.EventIDByName[event] = &id
		}
		cache.EventNameByID[cache.EventIDByName[event].String()] = event
	}
	common := p.svc.Common
	cs := p.svc.CampaignService
	cs.MailService = &Email{Common: common, EmailRepository: &repository.Email{DB: p.db}}
	cs.SMTPConfigService = &SMTPConfiguration{Common: common, SMTPConfigurationRepository: &repository.SMTPConfiguration{DB: p.db}}
	cs.DomainService = &Domain{Common: common, DomainRepository: &repository.Domain{DB: p.db}}
	cs.TemplateService = &Template{Common: common}

	f := &languageSendFixture{programTest: p, smtp: newFakeSMTP(t)}
	now := time.Now().UTC()
	domainID, smtpID, urlID, stateID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	p.create(&database.Domain{ID: domainID, CreatedAt: &now, UpdatedAt: &now, Name: "language.example.com"})
	p.create(&database.SMTPConfiguration{ID: smtpID, CreatedAt: &now, UpdatedAt: &now,
		Name: "smtp language", Host: "127.0.0.1", Port: uint16(f.smtp.port())})
	p.create(&database.Identifier{ID: &urlID, Name: "id"})
	p.create(&database.Identifier{ID: &stateID, Name: "s"})
	email := func(marker, subject string) uuid.UUID {
		id := uuid.New()
		p.create(&database.Email{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "email " + marker,
			Content: "body-" + marker + " {{.FirstName}}", MailFrom: "bounce@" + marker + ".example.com",
			From: "Sender <sender@" + marker + ".example.com>", Subject: subject})
		return id
	}
	page := func() uuid.UUID {
		id := uuid.New()
		p.create(&database.Page{ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "page " + id.String()[:8], Content: "<p>page</p>"})
		return id
	}
	f.template = uuid.New()
	p.create(&database.CampaignTemplate{
		ID: &f.template, CreatedAt: &now, UpdatedAt: &now, Name: "language template",
		DomainID: &domainID, SMTPConfigurationID: &smtpID, URLIdentifierID: &urlID, StateIdentifierID: &stateID,
		URLPath: "/", IsUsable: true, Language: "en",
	})
	enEmail := email("en", "Hello")
	landing := page()
	p.setTemplate(f.template, map[string]any{"email_id": enEmail, "landing_page_id": landing})
	f.frEmail = email("fr", "Bonjour")
	for code, emailID := range map[string]uuid.UUID{"de": email("de", "Hallo"), "fr": f.frEmail} {
		rowID, pageID := uuid.New(), page()
		p.create(&database.CampaignTemplateLanguage{
			ID: &rowID, CreatedAt: &now, UpdatedAt: &now, CampaignTemplateID: &f.template,
			Language: code, EmailID: &emailID, LandingPageID: &pageID, IsUsable: true,
		})
	}
	return f
}

// normalCampaign stores an open campaign that is not a program campaign
func (f *languageSendFixture) normalCampaign() uuid.UUID {
	f.t.Helper()
	now := time.Now().UTC()
	id := uuid.New()
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	f.create(&database.Campaign{
		ID: &id, CreatedAt: &now, UpdatedAt: &now, Name: "language campaign " + id.String()[:8],
		CampaignTemplateID: &f.template, SendStartAt: &start, SendEndAt: &end,
	})
	return id
}

// due adds a due row for a new recipient stamped with language, "" for none
func (f *languageSendFixture) due(campaignID uuid.UUID, name string, language string) uuid.UUID {
	f.t.Helper()
	rid := f.recipient(name + "@example.com")
	id := f.sendAt(campaignID, rid, time.Now().Add(-time.Minute), false)
	fields := map[string]any{"campaign_template_id": f.template.String()}
	if language != "" {
		fields["language"] = language
	}
	if err := f.db.Model(&database.CampaignRecipient{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		f.t.Fatalf("stamp row: %v", err)
	}
	return id
}

// sendDue runs the sender for every due row of the campaign, the way the
// batch reads them
func (f *languageSendFixture) sendDue(campaignID uuid.UUID) error {
	f.t.Helper()
	rows, err := f.svc.CampaignRecipientRepository.GetUnsendRecipientsForSending(
		f.ctx, 1000, &repository.CampaignRecipientOption{WithRecipient: true},
	)
	if err != nil {
		f.t.Fatalf("read due rows: %v", err)
	}
	campaign, err := f.svc.CampaignRepository.GetByID(f.ctx, &campaignID, &repository.CampaignOption{IncludeProgramManaged: true})
	if err != nil {
		f.t.Fatalf("load campaign: %v", err)
	}
	return f.svc.CampaignService.sendCampaignMessagesWithTemplate(f.ctx, authorizedSession(f.t), campaign, f.template, rows)
}

// TestSendMixedLanguagesUsesOneConnection checks rows of one template in
// three languages are sent over one SMTP connection, each with the email of
// its stamped language, and a row with no stamp gets the template language
func TestSendMixedLanguagesUsesOneConnection(t *testing.T) {
	f := newLanguageSendFixture(t)
	campaignID := f.normalCampaign()
	rows := map[uuid.UUID]string{
		f.due(campaignID, "alice", ""):   "body-en",
		f.due(campaignID, "bob", "de"):   "body-de",
		f.due(campaignID, "carol", "fr"): "body-fr",
		f.due(campaignID, "dave", "en"):  "body-en",
		f.due(campaignID, "erin", "de"):  "body-de",
	}
	if err := f.sendDue(campaignID); err != nil {
		t.Fatalf("send: %v", err)
	}
	conns, messages := f.smtp.counts()
	if conns != 1 {
		t.Fatalf("want one SMTP connection for the template, got %d", conns)
	}
	if len(messages) != len(rows) {
		t.Fatalf("want %d messages, got %d", len(rows), len(messages))
	}
	for id, marker := range rows {
		row := f.campaignRecipient(id)
		if row.SentAt == nil {
			t.Fatalf("row %v not sent", id)
		}
		var recipient database.Recipient
		f.db.Where("id = ?", row.RecipientID).First(&recipient)
		found := false
		for _, m := range messages {
			if strings.Contains(m, *recipient.Email) {
				found = true
				if !strings.Contains(m, marker) {
					t.Fatalf("%s got the wrong language, want %s", *recipient.Email, marker)
				}
			}
		}
		if !found {
			t.Fatalf("no message to %s", *recipient.Email)
		}
	}
}

// TestSendBrokenLanguageClosesCampaign checks a language whose email does not
// parse closes a normal campaign before anything is sent, like a broken
// template email
func TestSendBrokenLanguageClosesCampaign(t *testing.T) {
	f := newLanguageSendFixture(t)
	f.db.Model(&database.Email{}).Where("id = ?", f.frEmail).Update("subject", "Bonjour {{.FirstName")
	campaignID := f.normalCampaign()
	english := f.due(campaignID, "alice", "")
	french := f.due(campaignID, "carol", "fr")
	_ = f.sendDue(campaignID)
	if row := f.campaignRow(campaignID); row.ClosedAt == nil {
		t.Fatal("a broken language must close the campaign")
	}
	if conns, _ := f.smtp.counts(); conns != 0 {
		t.Fatalf("nothing may be sent, got %d connections", conns)
	}
	for _, id := range []uuid.UUID{english, french} {
		if row := f.campaignRecipient(id); row.SentAt != nil {
			t.Fatalf("row %v was sent", id)
		}
	}
}

// TestSendMissingLanguageClosesCampaign checks a row stamped with a language
// the template no longer has closes a normal campaign and is never sent the
// template language
func TestSendMissingLanguageClosesCampaign(t *testing.T) {
	f := newLanguageSendFixture(t)
	campaignID := f.normalCampaign()
	row := f.due(campaignID, "zoe", "sv")
	_ = f.sendDue(campaignID)
	if c := f.campaignRow(campaignID); c.ClosedAt == nil {
		t.Fatal("a missing language must close the campaign")
	}
	if r := f.campaignRecipient(row); r.SentAt != nil {
		t.Fatal("a row of a missing language was sent")
	}
	if conns, _ := f.smtp.counts(); conns != 0 {
		t.Fatalf("nothing may be sent, got %d connections", conns)
	}
}

// TestSendBrokenLanguageHoldsOnlyItsProgramRows checks a program campaign
// with a broken language holds only the rows of that language and sends the
// other languages over one connection, and the campaign stays open
func TestSendBrokenLanguageHoldsOnlyItsProgramRows(t *testing.T) {
	f := newLanguageSendFixture(t)
	f.db.Model(&database.Email{}).Where("id = ?", f.frEmail).Update("content", "Bonjour {{.FirstName")
	programID := f.program(model.ProgramLoopComplete)
	stepID := uuid.New()
	now := time.Now().UTC()
	f.create(&database.ProgramStep{ID: &stepID, CreatedAt: &now, UpdatedAt: &now,
		ProgramID: &programID, CampaignTemplateID: &f.template, Position: 0})
	campaignID := f.stepCampaign(programID, stepID, 0)
	english := f.due(campaignID, "alice", "")
	german := f.due(campaignID, "bob", "de")
	french := f.due(campaignID, "carol", "fr")
	if err := f.sendDue(campaignID); err != nil {
		t.Fatalf("a held language is no error: %v", err)
	}
	if row := f.campaignRow(campaignID); row.ClosedAt != nil {
		t.Fatal("the hidden campaign must stay open")
	}
	if row := f.campaignRecipient(french); row.CancelledAt == nil || row.SentAt != nil || row.LastAttemptAt != nil {
		t.Fatalf("the French row must be held before it is tried, cancelled %v sent %v tried %v",
			row.CancelledAt, row.SentAt, row.LastAttemptAt)
	}
	for _, id := range []uuid.UUID{english, german} {
		if row := f.campaignRecipient(id); row.SentAt == nil || row.CancelledAt != nil {
			t.Fatalf("row %v of a working language must be sent", id)
		}
	}
	if conns, messages := f.smtp.counts(); conns != 1 || len(messages) != 2 {
		t.Fatalf("want 2 messages on one connection, got %d on %d", len(messages), conns)
	}
}

// TestManualSendAndEmailBodyUseStampedLanguage checks the copied email body
// and the manual send read the email of the row's language
func TestManualSendAndEmailBodyUseStampedLanguage(t *testing.T) {
	f := newLanguageSendFixture(t)
	campaignID := f.normalCampaign()
	german := f.due(campaignID, "bob", "de")
	missing := f.due(campaignID, "zoe", "sv")
	f.svc.CampaignService.CampaignTemplateService.CampaignTemplateRepository = &repository.CampaignTemplate{DB: f.db}
	body, err := f.svc.CampaignService.GetCampaignEmailBody(f.ctx, authorizedSession(t), &german)
	if err != nil {
		t.Fatalf("email body: %v", err)
	}
	if !strings.Contains(body, "body-de") {
		t.Fatalf("email body is not German: %s", body)
	}
	if _, err := f.svc.CampaignService.GetCampaignEmailBody(f.ctx, authorizedSession(t), &missing); !isValidationError(err) {
		t.Fatalf("missing language: want validation error, got %v", err)
	}

	cr, err := f.svc.CampaignRecipientRepository.GetByID(f.ctx, &german, &repository.CampaignRecipientOption{WithRecipient: true})
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if err := f.svc.CampaignService.sendSingleCampaignMessage(f.ctx, authorizedSession(t), &campaignID, cr); err != nil {
		t.Fatalf("manual send: %v", err)
	}
	if _, messages := f.smtp.counts(); len(messages) != 1 || !strings.Contains(messages[0], "body-de") {
		t.Fatalf("manual send is not German: %v", messages)
	}
	cr, err = f.svc.CampaignRecipientRepository.GetByID(f.ctx, &missing, &repository.CampaignRecipientOption{WithRecipient: true})
	if err != nil {
		t.Fatalf("load row: %v", err)
	}
	if err := f.svc.CampaignService.sendSingleCampaignMessage(f.ctx, authorizedSession(t), &campaignID, cr); err == nil {
		t.Fatal("a manual send in a missing language must fail")
	}
	if _, messages := f.smtp.counts(); len(messages) != 1 {
		t.Fatalf("a missing language was sent: %d messages", len(messages))
	}
}
