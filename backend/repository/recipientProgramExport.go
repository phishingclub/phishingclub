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
}

// programSendJoins joins a program's hidden campaign to its program and its
// template, starting from the given table and its campaign_id column. Only
// programs that are not anonymous are joined, an anonymous program has no per
// person data.
func programSendJoins(from string) clause.From {
	campaignCol := func(name string) clause.Column {
		return clause.Column{Table: database.CAMPAIGN_TABLE, Name: name}
	}
	programCol := func(name string) clause.Column {
		return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
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
				Column: campaignCol("campaign_template_id"),
				Value:  clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"},
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
		Clauses(programSendJoins(database.CAMPAIGN_EVENT_TABLE), clause.Select{Columns: columns}).
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
	)
	rows := []RecipientProgramSend{}
	res := r.DB.WithContext(ctx).
		Session(&gorm.Session{NewDB: true}).
		Table(table).
		Clauses(programSendJoins(table), clause.Select{Columns: columns}).
		Where(clause.Eq{Column: col("recipient_id"), Value: recipientID}).
		Where(clause.Neq{Column: col("sent_at"), Value: nil}).
		Order(clause.OrderByColumn{Column: col("sent_at")}).
		Scan(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return rows, nil
}
