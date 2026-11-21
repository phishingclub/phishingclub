package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/wneessen/go-mail"
	"gorm.io/gorm"
)

// how often the due check runs, how long a failed program waits before the
// next try, and how long one pass may take
const (
	programReportCheckEvery = time.Minute
	programReportRetryAfter = time.Hour
	programReportRunTimeout = 30 * time.Minute
)

// programReportTrigger is the send log trigger of an automated program report
const programReportTrigger = "scheduled"

// errProgramReportNotSetUp is a report that is not sent because report
// delivery is off or not set up for the program's company
var errProgramReportNotSetUp = errors.New("report delivery is not set up for this company")

// programReportState is the in memory state of the automated report sender
type programReportState struct {
	mu        sync.Mutex
	running   bool
	lastCheck time.Time
	// retryAt holds back a program whose last try failed
	retryAt map[uuid.UUID]time.Time
	// failNoted is the period end whose failure was already written to the
	// send log and the audit log, so a retry does not write it again
	failNoted map[uuid.UUID]time.Time
}

// SendDueReports starts a background pass that emails every automated program
// report that is due. It returns at once, a pass runs at most once a minute
// and never two at the same time. The pass ends when ctx ends or after
// programReportRunTimeout, so ctx must outlive one task runner tick.
func (s *Program) SendDueReports(ctx context.Context, session *model.Session) error {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.SendDueReports", session)
		s.AuditLogNotAuthorized(ae)
		return errs.ErrAuthorizationFailed
	}
	st := &s.reports
	st.mu.Lock()
	now := time.Now()
	if st.running || now.Sub(st.lastCheck) < programReportCheckEvery {
		st.mu.Unlock()
		return nil
	}
	st.running = true
	st.lastCheck = now
	st.mu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Errorw("recovered from panic while sending program reports", "panic", r)
			}
			st.mu.Lock()
			st.running = false
			st.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(ctx, programReportRunTimeout)
		defer cancel()
		s.sendDueReports(ctx, session, time.Now())
	}()
	return nil
}

// programReportSettleTimeout bounds giving back or finishing a report claim
// after the send, which runs also when the pass ran out of time
const programReportSettleTimeout = 10 * time.Second

// programReportClosedRecheck is how long a closed program with no report
// left to send is skipped before it is read again
const programReportClosedRecheck = 24 * time.Hour

// programReportClaimStale is how long a claimed report period may stay unsent
// before it is given back. A send takes far less, a claim that old was left
// by a process that stopped between the claim and the send.
const programReportClaimStale = time.Hour

// sendDueReports sends the due report of every program, one at a time
func (s *Program) sendDueReports(ctx context.Context, session *model.Session, now time.Time) {
	// the run reads every report program in turn, the facts it reads are not
	// kept so they do not push out the facts of the programs people read
	ctx = withProgramFactsBackground(ctx)
	if !s.programReportEnabled(ctx) {
		return
	}
	// a period claimed by a pass that stopped before the send is given back
	if n, err := s.ProgramRepository.GiveBackStaleReportClaims(ctx, now.Add(-programReportClaimStale)); err != nil {
		s.Logger.Errorw("failed to give back stale automated report periods", "error", err)
	} else if n > 0 {
		s.Logger.Warnw("gave back automated report periods that were claimed and never sent", "count", n)
	}
	ids, err := s.ProgramRepository.GetReportEnabledIDs(ctx)
	if err != nil {
		s.Logger.Errorw("failed to list programs with automated reports", "error", err)
		return
	}
	st := &s.reports
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		st.mu.Lock()
		retry, wait := st.retryAt[*id]
		st.mu.Unlock()
		if wait && now.Before(retry) {
			continue
		}
		program, err := s.ProgramRepository.GetByID(ctx, id, &repository.ProgramOption{
			WithSteps:           true,
			WithRecipientGroups: true,
		})
		if err != nil {
			s.Logger.Errorw("failed to read program for its automated report", "programID", id.String(), "error", err)
			continue
		}
		// a delete or close may have started since the ids were read
		if program.DeletingAt != nil || program.ClosingAt != nil {
			continue
		}
		// a report waits while an anonymize of the program runs, the
		// report is still due when it ends and goes on a later pass
		if programAnonymizeRunning(program) || s.isAnonymizingNow(*id) {
			continue
		}
		program.SetReadState(now)
		from, to, ok := programReportDuePeriod(program, now)
		if !ok {
			// a closed program with no report left to send, such as one
			// closed before reports were turned on, is read once a day
			// instead of on every pass
			if program.IsClosed() && program.ReportNextAtTime(now) == nil {
				st.mu.Lock()
				if st.retryAt == nil {
					st.retryAt = map[uuid.UUID]time.Time{}
				}
				st.retryAt[*id] = now.Add(programReportClosedRecheck)
				st.mu.Unlock()
			}
			continue
		}
		err = s.sendProgramReport(ctx, session, program, from, to, now)
		st.mu.Lock()
		if err != nil {
			if st.retryAt == nil {
				st.retryAt = map[uuid.UUID]time.Time{}
			}
			st.retryAt[*id] = now.Add(programReportRetryAfter)
		} else {
			delete(st.retryAt, *id)
			delete(st.failNoted, *id)
		}
		st.mu.Unlock()
	}
}

// programReportDuePeriod returns the automated report period that is due at
// now. An anonymous program counts a month once it has settled, so its report
// waits until the whole period has settled.
func programReportDuePeriod(program *model.Program, now time.Time) (time.Time, time.Time, bool) {
	if program.IsAnonymousOn() {
		now = programSettleCutoff(now, program.Location())
	}
	return program.ReportDuePeriod(now)
}

// noteFailure reports whether the failure of the period ending at to is the
// first one, and marks it as noted
func (st *programReportState) noteFailure(id uuid.UUID, to time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if v, ok := st.failNoted[id]; ok && v.Equal(to) {
		return false
	}
	if st.failNoted == nil {
		st.failNoted = map[uuid.UUID]time.Time{}
	}
	st.failNoted[id] = to
	return true
}

// programReportPeriodText names a report period [from, to) in the program zone
func programReportPeriodText(from, to time.Time, loc *time.Location) string {
	return from.In(loc).Format("2 Jan 2006") + " to " + to.Add(-time.Nanosecond).In(loc).Format("2 Jan 2006")
}

// errProgramReportAnonymizeBegan stops a report whose program began to be
// anonymized while the report was built
var errProgramReportAnonymizeBegan = errors.New("an anonymize of the program began while its report was built")

// sendProgramReport renders the program report of [from, to) and emails it to
// the report recipients of the program's company, the same people a campaign
// report goes to. The period is claimed before the email is sent and given
// back when the send fails, so a period is never sent twice.
func (s *Program) sendProgramReport(
	ctx context.Context,
	session *model.Session,
	program *model.Program,
	from, to time.Time,
	now time.Time,
) (err error) {
	ae := NewAuditEvent("Program.SendReport", session)
	defer s.auditDone(ae, &err)
	id := program.ID.MustGet()
	loc := program.Location()
	ae.Details["id"] = id.String()
	ae.Details["from"] = from.UTC().Format(time.RFC3339)
	ae.Details["to"] = to.UTC().Format(time.RFC3339)
	name := programName(program)
	groupName := ""
	senderEmailText := ""
	// failed writes the send log and the audit log once per period, a retry
	// of the same period only logs
	failed := func(recipients []string, cause error) error {
		if s.reports.noteFailure(id, to) {
			s.writeProgramReportLog(ctx, program, name, groupName, senderEmailText, "failed", recipients, cause)
			ae.Details["outcome"] = "failed"
			s.AuditLogFailed(ae, cause)
		} else {
			// the failure of this period is already in the audit log
			ae.skip()
		}
		s.Logger.Warnw("automated program report not sent", "programID", id.String(), "error", cause)
		return errs.Wrap(cause)
	}
	// an anonymous report covers whole calendar months only
	if program.IsAnonymousOn() && (!isMonthStart(from, loc) || !isMonthStart(to, loc)) {
		return failed(nil, errs.NewValidationError(errProgramReportWholeMonths))
	}
	if s.CompanyReportConfigRepository == nil || s.ReportSendLogRepository == nil || s.SMTPConfigService == nil {
		ae.Details["reason"] = "notSetUp"
		return failed(nil, errProgramReportNotSetUp)
	}
	config, err := s.programReportDelivery(ctx, optionalID(program.CompanyID))
	if errors.Is(err, errProgramReportNotSetUp) {
		ae.Details["reason"] = "notSetUp"
		return failed(nil, err)
	}
	if err != nil {
		return failed(nil, fmt.Errorf("failed to read the report delivery settings: %w", err))
	}
	if err := config.Validate(); err != nil {
		return failed(nil, err)
	}
	groupID := config.RecipientGroupID.MustGet()
	smtpConfigID := config.SMTPConfigurationID.MustGet()
	senderEmail := config.SenderEmail.MustGet()
	senderEmailText = senderEmail.String()
	if group, gErr := s.RecipientGroupRepository.GetByID(ctx, &groupID, &repository.RecipientGroupOption{}); gErr == nil && group != nil {
		if n, nErr := group.Name.Get(); nErr == nil {
			groupName = n.String()
		}
	}
	recipients, err := s.RecipientGroupRepository.GetRecipientsByGroupID(ctx, &groupID, &repository.RecipientOption{})
	if err != nil {
		return failed(nil, errors.New("the configured recipient group is unavailable"))
	}
	toEmails := []string{}
	for _, r := range recipients.Rows {
		if email, eErr := r.Email.Get(); eErr == nil {
			toEmails = append(toEmails, email.String())
		}
	}
	if len(toEmails) == 0 {
		return failed(nil, errors.New("the recipient group has no recipients"))
	}
	smtpConfig, err := s.SMTPConfigService.GetByID(ctx, session, &smtpConfigID, &repository.SMTPConfigurationOption{WithHeaders: true})
	if err != nil {
		return failed(nil, fmt.Errorf("smtp configuration unavailable: %w", err))
	}
	// the report is rendered before the period is claimed, a render failure
	// leaves the period to the next try
	htmlContent, err := s.buildReportHTML(ctx, session, program, now, &from, &to)
	if err != nil {
		return failed(nil, fmt.Errorf("failed to build report: %w", err))
	}
	rb := newReportBrowser(ctx, s.RemoteBrowserExecPath, s.Logger)
	// the deferred close gives the browser slot back also after a panic
	defer rb.close()
	pdf, err := rb.renderPDF(htmlContent)
	// the browser is not needed while the email is sent
	rb.close()
	if err != nil {
		return failed(nil, fmt.Errorf("failed to render report PDF: %w", err))
	}
	// an Anonymize All that began while the report was built or rendered changes how the
	// results are read, so the numbers could mix both ways. The report is
	// dropped without a log and goes on a later pass.
	if after, err := s.ProgramRepository.GetByID(ctx, &id, &repository.ProgramOption{}); err != nil ||
		after.AnonymizeBegun() != program.AnonymizeBegun() || s.isAnonymizingNow(id) {
		ae.skip()
		return errProgramReportAnonymizeBegan
	}
	periodText := programReportPeriodText(from, to, loc)
	m := mail.NewMsg(mail.WithNoDefaultUserAgent())
	if err := m.EnvelopeFrom(senderEmailText); err != nil {
		return failed(nil, fmt.Errorf("the report sender email address is invalid: %w", err))
	}
	setMessageIDFromAddress(m, senderEmailText)
	if err := m.From(senderEmailText); err != nil {
		return failed(nil, fmt.Errorf("the report sender email address is invalid: %w", err))
	}
	if err := m.To(toEmails...); err != nil {
		return failed(nil, fmt.Errorf("a report recipient email address is invalid: %w", err))
	}
	applyCustomSMTPHeaders(m, smtpConfig.Headers, nil, nil, s.Logger)
	subject, body := s.programReportEmail(config, programReportEmailData{
		ProgramName:  name,
		CampaignName: name,
		CompanyName:  s.programCompanyName(ctx, program),
		Period:       periodText,
		ReportDate:   now.In(loc).Format("2006-01-02"),
	})
	m.Subject(subject)
	m.SetBodyString("text/html", body)
	filename := fmt.Sprintf("program-report-%s-%s.pdf", sanitizeReportFilenameOr(name, "program"), to.Add(-time.Nanosecond).In(loc).Format("2006-01"))
	if err := m.AttachReader(filename, bytes.NewReader(pdf)); err != nil {
		return failed(nil, err)
	}
	// claim the period, another pass that claimed it first sends it
	prev := program.ReportLastPeriodEnd
	claimed, err := s.ProgramRepository.ClaimReportPeriod(ctx, &id, prev, &to)
	if err != nil {
		return failed(nil, fmt.Errorf("failed to claim the report period: %w", err))
	}
	if !claimed {
		return nil
	}
	// the claim is given back or finished also when the pass ran out of time
	// during the send, so the period is neither lost nor sent twice
	settle := func(fn func(context.Context) error) error {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), programReportSettleTimeout)
		defer cancel()
		return fn(sctx)
	}
	if err := s.SMTPConfigService.SendMessages(ctx, smtpConfig, m); err != nil {
		// give the period back so the next try sends it
		rErr := settle(func(sctx context.Context) error {
			_, err := s.ProgramRepository.GiveBackReportPeriod(sctx, &id, &to, prev)
			return err
		})
		if rErr != nil {
			s.Logger.Errorw("failed to give back an automated report period", "programID", id.String(), "error", rErr)
		}
		return failed(toEmails, err)
	}
	// a claim that is not marked as sent is given back after
	// programReportClaimStale and the period is sent again
	fErr := settle(func(sctx context.Context) error {
		return s.ProgramRepository.FinishReportClaim(sctx, &id, &to)
	})
	if fErr != nil {
		s.Logger.Errorw("failed to mark an automated report period as sent", "programID", id.String(), "error", fErr)
	}
	_ = settle(func(sctx context.Context) error {
		s.writeProgramReportLog(sctx, program, name, groupName, senderEmailText, "sent", toEmails, nil)
		return nil
	})
	ae.Details["outcome"] = "sent"
	ae.Details["recipients"] = len(toEmails)
	s.AuditLogAuthorized(ae)
	return nil
}

// the subject and body of an automated program report email when the report
// delivery config sets none
const (
	defaultProgramReportEmailSubject = "Program report: {{.ProgramName}}, {{.Period}}"
	defaultProgramReportEmailBody    = "<p>The program report for {{.ProgramName}} covering {{.Period}} is attached.</p>"
)

// programReportEmailData are the variables of the subject and body of an
// automated program report email. CampaignName holds the program name too, so
// a subject written for campaign reports names the program. A variable a
// program report does not have, such as a campaign rate, fails the render and
// the default is used.
type programReportEmailData struct {
	ProgramName  string
	CampaignName string
	CompanyName  string
	Period       string
	ReportDate   string
}

// programReportEmail renders the subject and body of an automated program
// report email from the report delivery config, the same fields a campaign
// report uses, with the program defaults when a field is empty or does not
// render. The subject is a plain text mail header and reads the data as it
// is, the body is html and reads it escaped.
func (s *Program) programReportEmail(config *model.CompanyReportConfig, d programReportEmailData) (string, string) {
	field := func(v nullable.Nullable[string], fallback string) string {
		if t, err := v.Get(); err == nil && strings.TrimSpace(t) != "" {
			return t
		}
		return fallback
	}
	escaped := programReportEmailData{
		ProgramName:  html.EscapeString(d.ProgramName),
		CampaignName: html.EscapeString(d.CampaignName),
		CompanyName:  html.EscapeString(d.CompanyName),
		Period:       html.EscapeString(d.Period),
		ReportDate:   html.EscapeString(d.ReportDate),
	}
	subject := s.renderProgramReportEmailField(field(config.EmailSubject, defaultProgramReportEmailSubject), defaultProgramReportEmailSubject, d)
	body := s.renderProgramReportEmailField(field(config.EmailBody, defaultProgramReportEmailBody), defaultProgramReportEmailBody, escaped)
	return subject, body
}

// renderProgramReportEmailField renders a subject or body template with the
// template functions of a campaign report, and the fallback when it fails to
// parse or render, so a broken custom template never stops a delivery
func (s *Program) renderProgramReportEmailField(tmpl, fallback string, data programReportEmailData) string {
	render := func(v string) (string, error) {
		t, err := texttemplate.New("programReportEmail").Funcs(TemplateFuncs()).Parse(v)
		if err != nil {
			return "", err
		}
		var b bytes.Buffer
		if err := t.Execute(&b, data); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	out, err := render(tmpl)
	if err != nil {
		s.Logger.Warnw("failed to render program report email template, using default", "error", err)
		if out, err = render(fallback); err != nil {
			return ""
		}
	}
	return out
}

// programCompanyName returns the name of the company of a program, empty for
// a program without one or when it can not be read
func (s *Program) programCompanyName(ctx context.Context, program *model.Program) string {
	companyID, err := program.CompanyID.Get()
	if err != nil || s.CompanyRepository == nil {
		return ""
	}
	c, err := s.CompanyRepository.GetByID(ctx, &companyID)
	if err != nil {
		return ""
	}
	if v, err := c.Name.Get(); err == nil {
		return v.String()
	}
	return ""
}

// programReportDelivery returns the delivery config a program report of the
// company scope is sent with. A company program needs the company's own
// delivery config, enabled, like a campaign. The global config can not be
// enabled, it only holds default fields, so a program without a company is sent
// with those fields and its own switch is the opt in. It returns
// errProgramReportNotSetUp when the scope has no usable config.
func (s *Program) programReportDelivery(ctx context.Context, companyID *uuid.UUID) (*model.CompanyReportConfig, error) {
	companyConfig, err := s.CompanyReportConfigRepository.GetByCompanyID(ctx, companyID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && companyID != nil && !companyConfig.Enabled) {
		return nil, errProgramReportNotSetUp
	}
	if err != nil {
		return nil, errs.Wrap(err)
	}
	var globalConfig *model.CompanyReportConfig
	if g, gErr := s.CompanyReportConfigRepository.GetByCompanyID(ctx, nil); gErr == nil {
		globalConfig = g
	} else if !errors.Is(gErr, gorm.ErrRecordNotFound) {
		return nil, errs.Wrap(gErr)
	}
	return programReportDeliveryConfig(companyConfig, globalConfig), nil
}

// ProgramReportDeliveryStatus tells the program wizard whether automated
// reports of a scope can be sent. Reason is "pdfDisabled", "notSetUp" or
// "incomplete" when they can not.
type ProgramReportDeliveryStatus struct {
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// GetReportDeliveryStatus checks the report delivery of the company scope the
// same way the automated report sender does. A nil company is the global
// scope.
func (s *Program) GetReportDeliveryStatus(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
) (*ProgramReportDeliveryStatus, error) {
	isAuthorized, err := IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		s.LogAuthError(err)
		return nil, errs.Wrap(err)
	}
	if !isAuthorized {
		ae := NewAuditEvent("Program.GetReportDeliveryStatus", session)
		s.AuditLogNotAuthorized(ae)
		return nil, errs.ErrAuthorizationFailed
	}
	if s.CompanyReportConfigRepository == nil {
		return &ProgramReportDeliveryStatus{Reason: "notSetUp"}, nil
	}
	if !s.programReportEnabled(ctx) {
		return &ProgramReportDeliveryStatus{Reason: "pdfDisabled"}, nil
	}
	config, err := s.programReportDelivery(ctx, companyID)
	if errors.Is(err, errProgramReportNotSetUp) {
		return &ProgramReportDeliveryStatus{Reason: "notSetUp"}, nil
	}
	if err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return &ProgramReportDeliveryStatus{Reason: "incomplete"}, nil
	}
	return &ProgramReportDeliveryStatus{Ready: true}, nil
}

// programReportDeliveryConfig merges the company and global delivery configs
// for a program report. The global config is never enabled, so the merged
// config is marked enabled here: Validate then requires the recipient group,
// smtp config and sender, which are read with MustGet after it.
func programReportDeliveryConfig(company, global *model.CompanyReportConfig) *model.CompanyReportConfig {
	config := resolveEffectiveReportConfig(company, global)
	config.Enabled = true
	return config
}

// writeProgramReportLog writes a report send log row for a program report.
// Logging never stops a delivery, its own failure is only logged.
func (s *Program) writeProgramReportLog(
	ctx context.Context,
	program *model.Program,
	name, groupName, senderEmail, status string,
	recipients []string,
	cause error,
) {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	row := &model.ReportSendLog{
		CompanyID:      nullableUUIDPtr(optionalID(program.CompanyID)),
		CampaignID:     nullable.NewNullNullable[uuid.UUID](),
		ProgramID:      nullable.NewNullableWithValue(program.ID.MustGet()),
		CampaignName:   name,
		GroupName:      groupName,
		Trigger:        programReportTrigger,
		Status:         status,
		RecipientCount: len(recipients),
		Recipients:     strings.Join(recipients, ", "),
		SenderEmail:    senderEmail,
		ErrorMessage:   msg,
	}
	// without a send log only the audit log and the error log hold the outcome
	if s.ReportSendLogRepository == nil {
		return
	}
	if _, err := s.ReportSendLogRepository.Insert(ctx, row); err != nil {
		s.Logger.Errorw("failed to write program report send log", "error", err, "programID", program.ID.MustGet().String())
	}
}
