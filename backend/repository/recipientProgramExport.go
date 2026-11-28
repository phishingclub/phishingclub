package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// RecipientProgramEvent is one event of a recipient in a program send, named
// by the program and the template of the send
type RecipientProgramEvent struct {
	CreatedAt        *time.Time
	ProgramID        *uuid.UUID
	ProgramName      string
	ProgramCompanyID *uuid.UUID
	TemplateName     string
	EventID          *uuid.UUID
	IPAddress        string
	UserAgent        string
	Data             string
}

// RecipientProgramSend is one program send to a recipient
type RecipientProgramSend struct {
	ProgramID        *uuid.UUID
	ProgramName      string
	ProgramCompanyID *uuid.UUID
	TemplateName     string
	IsTraining       bool
	SentAt           *time.Time
	NotableEventID   *uuid.UUID
	// Language is the language stamp of the send, nil when it has none
	Language *string
	// TemplateLanguage is the language of the template, empty when not set
	TemplateLanguage *string
}

// EffectiveLanguage returns the language the send was made in: its stamp, or
// the template language when it has none. Empty when neither is set.
func (s *RecipientProgramSend) EffectiveLanguage() string {
	if s.Language != nil && *s.Language != "" {
		return *s.Language
	}
	if s.TemplateLanguage != nil {
		return *s.TemplateLanguage
	}
	return ""
}

// programSendJoins joins a program's hidden campaign to its program and its
// template, starting from the given table and its campaign_id column. With
// rowTemplate the template is the row's own campaign_template_id, else the
// hidden campaign's. Only programs that are not anonymous are joined, an
// anonymous program has no per person data.
func programSendJoins(from string, rowTemplate bool) clause.From {
	campaignCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	programCol := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
	}
	var templateID any = campaignCol("campaign_template_id")
	if rowTemplate {
		templateID = clause.Expr{SQL: "COALESCE(?, ?)", Vars: []any{
			clause.Column{Table: from, Name: "campaign_template_id"},
			campaignCol("campaign_template_id"),
		}}
	}
	return clause.From{Joins: []clause.Join{
		{
			Type:  clause.InnerJoin,
			Table: clause.Table{Name: database.CAMPAIGN_TABLE},
			ON: clause.Where{Exprs: []clause.Expression{clause.Eq{
				Column: clause.Column{Table: from, Name: "campaign_id"},
				Value:  campaignCol("id"),
			}}},
		},
		{
			Type:  clause.InnerJoin,
			Table: clause.Table{Name: database.PROGRAM_TABLE},
			ON: clause.Where{Exprs: []clause.Expression{
				clause.Eq{Column: campaignCol("program_id"), Value: programCol("id")},
				clause.Eq{Column: programCol("is_anonymous"), Value: false},
				programNotAnonymizing(database.PROGRAM_TABLE),
			}},
		},
		{
			Type:  clause.LeftJoin,
			Table: clause.Table{Name: database.CAMPAIGN_TEMPLATE_TABLE},
			ON: clause.Where{Exprs: []clause.Expression{clause.Eq{
				Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"},
				Value:  templateID,
			}}},
		},
	}}
}

// programSendColumns are the program columns read with programSendJoins
func programSendColumns() []clause.Column {
	return []clause.Column{
		{Table: database.PROGRAM_TABLE, Name: "id", Alias: "program_id"},
		{Table: database.PROGRAM_TABLE, Name: "name", Alias: "program_name"},
		{Table: database.PROGRAM_TABLE, Name: "company_id", Alias: "program_company_id"},
		{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "name", Alias: "template_name"},
	}
}

// GetProgramEventsForExport returns the events of a recipient in the sends of
// programs that are not anonymous, oldest first
func (r *Recipient) GetProgramEventsForExport(
	ctx context.Context,
	recipientID *uuid.UUID,
) ([]RecipientProgramEvent, error) {
	eventCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_EVENT_TABLE, Name: name}
	}
	columns := append(programSendColumns(),
		eventCol("created_at"),
		eventCol("event_id"),
		eventCol("ip_address"),
		eventCol("user_agent"),
		eventCol("data"),
	)
	rows := []RecipientProgramEvent{}
	res := r.DB.WithContext(ctx).
		Session(&gorm.Session{NewDB: true}).
		Table(database.CAMPAIGN_EVENT_TABLE).
		Clauses(programSendJoins(database.CAMPAIGN_EVENT_TABLE, false), clause.Select{Columns: columns}).
		Where(clause.Eq{Column: eventCol("recipient_id"), Value: recipientID}).
		Order(clause.OrderByColumn{Column: eventCol("created_at")}).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}

// GetProgramSendsForExport returns the sent program sends of a recipient in
// programs that are not anonymous, oldest first
func (r *Recipient) GetProgramSendsForExport(
	ctx context.Context,
	recipientID *uuid.UUID,
) ([]RecipientProgramSend, error) {
	table := database.CAMPAIGN_RECIPIENT_TABLE_NAME
	col := func(name string) clause.Column {
		return clause.Column{Table: table, Name: name}
	}
	columns := append(programSendColumns(),
		clause.Column{Table: database.CAMPAIGN_TABLE, Name: "is_training"},
		col("sent_at"),
		col("notable_event_id"),
		col("language"),
		clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "language", Alias: "template_language"},
	)
	rows := []RecipientProgramSend{}
	res := r.DB.WithContext(ctx).
		Session(&gorm.Session{NewDB: true}).
		Table(table).
		Clauses(programSendJoins(table, true), clause.Select{Columns: columns}).
		Where(clause.Eq{Column: col("recipient_id"), Value: recipientID}).
		Where(clause.Neq{Column: col("sent_at"), Value: nil}).
		Order(clause.OrderByColumn{Column: col("sent_at")}).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}
