package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var allowedProgramColumns = assignTableToColumns(database.PROGRAM_TABLE, []string{
	"created_at",
	"updated_at",
	"name",
	"start_at",
	"started_at",
	"paused_at",
})

// programSearchColumns are the text columns the program list search matches
var programSearchColumns = []string{"name"}

// likeEscape is the escape character of the program search LIKE pattern
const likeEscape = `\`

// likeEscaper escapes the LIKE wildcards and the escape character itself, so
// a search for Q1_2026 matches that text only
var likeEscaper = strings.NewReplacer(likeEscape, likeEscape+likeEscape, "%", likeEscape+"%", "_", likeEscape+"_")

// programSearch matches the programs whose search columns hold the search
// text. The LIKE wildcards in the text are matched as they are written.
func programSearch(search string) clause.Expression {
	pattern := "%" + likeEscaper.Replace(search) + "%"
	likes := make([]clause.Expression, 0, len(programSearchColumns))
	for _, column := range programSearchColumns {
		likes = append(likes, clause.Expr{
			SQL:  "? LIKE ? ESCAPE ?",
			Vars: []any{clause.Column{Table: database.PROGRAM_TABLE, Name: column}, pattern, likeEscape},
		})
	}
	return clause.Or(likes...)
}

// ProgramOption controls preloading and filtering of a program read.
type ProgramOption struct {
	*vo.QueryArgs
	WithSteps           bool
	WithRecipientGroups bool
	// WithLanguageRoutes loads the language routes, by GetByID only
	WithLanguageRoutes bool
	// Status limits a list to the programs of one of model.ProgramListStatuses,
	// empty lists every program
	Status string
	// AllCompanies lists the programs of every company when no company is
	// given, as the dashboard does. Without it no company means the programs
	// without a company.
	AllCompanies bool
	// WithCompany loads the company of each program
	WithCompany bool
}

// Program is the repository for programs, their steps, assigned groups,
// enrollments and the link to their hidden campaigns.
type Program struct {
	DB *gorm.DB
}

// ProgramChildren are the rows a program create or update writes together
// with the program row. A nil list is left as it is stored, an empty list
// removes every row.
type ProgramChildren struct {
	Steps             []*model.ProgramStep
	RecipientGroupIDs []*uuid.UUID
	AllowDenyIDs      []*uuid.UUID
	Webhooks          []*model.CampaignWebhook
	// LanguageRoutes are the normalized language routes, nil keeps them
	LanguageRoutes map[string]string
}

// programsColumn is a column of the programs table
func programsColumn(name string) clause.Column {
	return clause.Column{Table: database.PROGRAM_TABLE, Name: name}
}

// Insert stores a new program row with its steps, groups, allow or deny lists
// and webhooks in one transaction and returns its id.
func (r *Program) Insert(
	ctx context.Context,
	program *model.Program,
	children *ProgramChildren,
) (*uuid.UUID, error) {
	id := uuid.New()
	row := program.ToDBMap()
	row["id"] = id
	AddTimestamps(row)
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&database.Program{}).Create(row).Error; err != nil {
			return errs.Wrap(err)
		}
		return writeProgramChildren(tx, &id, children)
	})
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// UpdateByID updates the storable fields of a program, the columns in fields
// and the child rows given in children, in one transaction. fields and
// children may be nil.
func (r *Program) UpdateByID(
	ctx context.Context,
	id *uuid.UUID,
	program *model.Program,
	children *ProgramChildren,
	fields map[string]any,
) error {
	row := program.ToDBMap()
	for k, v := range fields {
		row[k] = v
	}
	AddUpdatedAt(row)
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// the first statement is a write, so the transaction holds the write
		// lock before it reads
		if err := tx.Model(&database.Program{}).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
			Updates(row).Error; err != nil {
			return errs.Wrap(err)
		}
		return writeProgramChildren(tx, id, children)
	})
}

// writeProgramChildren writes the child rows of a program inside a
// transaction
func writeProgramChildren(tx *gorm.DB, programID *uuid.UUID, children *ProgramChildren) error {
	if children == nil {
		return nil
	}
	if children.Steps != nil {
		if err := replaceStepsTx(tx, programID, children.Steps); err != nil {
			return err
		}
	}
	if children.RecipientGroupIDs != nil {
		if err := replaceRecipientGroupsTx(tx, programID, children.RecipientGroupIDs); err != nil {
			return err
		}
	}
	if children.AllowDenyIDs != nil {
		if err := replaceAllowDeniesTx(tx, programID, children.AllowDenyIDs); err != nil {
			return err
		}
	}
	if children.Webhooks != nil {
		if err := replaceWebhooksTx(tx, programID, children.Webhooks); err != nil {
			return err
		}
	}
	if children.LanguageRoutes != nil {
		if err := replaceProgramLanguageRoutesTx(tx, programID, children.LanguageRoutes); err != nil {
			return err
		}
	}
	return nil
}

// SetFields updates arbitrary columns on a program, used for lifecycle changes
// like pause, resume and close.
func (r *Program) SetFields(
	ctx context.Context,
	id *uuid.UUID,
	fields map[string]any,
) error {
	AddUpdatedAt(fields)
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Updates(fields)
	return errs.Wrap(res.Error)
}

// DeleteByID removes a program and its steps, groups, enrollments, campaign
// links, allow or deny lists and webhooks. The enrollments are removed in
// batches first, so a large program does not hold the database in one write.
// The rest, and any enrollment added in between, go in one transaction. The
// hidden campaigns are deleted by the program service before this.
func (r *Program) DeleteByID(
	ctx context.Context,
	id *uuid.UUID,
) error {
	if err := r.DeleteEnrollmentsByProgramID(ctx, id); err != nil {
		return err
	}
	byProgram := programIDEq(clause.Column{Name: "program_id"}, id)
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, table := range []any{
			&database.ProgramStep{},
			&database.ProgramRecipientGroup{},
			&database.ProgramEnrollment{},
			&database.ProgramCampaign{},
			&database.ProgramAllowDeny{},
			&database.ProgramWebhook{},
			&database.ProgramLanguageRoute{},
		} {
			if err := tx.Where(byProgram).Delete(table).Error; err != nil {
				return errs.Wrap(err)
			}
		}
		if err := tx.Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
			Delete(&database.Program{}).Error; err != nil {
			return errs.Wrap(err)
		}
		return nil
	})
}

// GetByID loads a program and optionally its steps and assigned groups.
func (r *Program) GetByID(
	ctx context.Context,
	id *uuid.UUID,
	options *ProgramOption,
) (*model.Program, error) {
	var row database.Program
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		First(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	program, err := ToProgram(&row)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if options != nil && options.WithSteps {
		steps, err := r.GetSteps(ctx, id)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		program.Steps = steps
	}
	if options != nil && options.WithRecipientGroups {
		groups, err := r.getRecipientGroups(ctx, id)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		program.RecipientGroups = groups
		ids := make([]*uuid.UUID, 0, len(groups))
		for _, g := range groups {
			gid := g.ID.MustGet()
			ids = append(ids, &gid)
		}
		program.RecipientGroupIDs = nullable.NewNullableWithValue(ids)
	}
	if options != nil && options.WithLanguageRoutes {
		routes, err := r.GetLanguageRoutesByProgramID(ctx, id)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		program.LanguageRoutes = nullable.NewNullableWithValue(routes)
	}
	return program, nil
}

// GetAll lists programs scoped to a company, optionally only those of one
// list status.
func (r *Program) GetAll(
	ctx context.Context,
	companyID *uuid.UUID,
	options *ProgramOption,
	now time.Time,
) (*model.Result[model.Program], error) {
	result := model.NewEmptyResult[model.Program]()
	db := r.DB.WithContext(ctx)
	if companyID != nil || !options.AllCompanies {
		db = whereCompany(db, database.PROGRAM_TABLE, companyID)
	}
	if options.WithCompany {
		db = db.Preload("Company")
	}
	db, err := r.whereListStatus(db, options.Status, now)
	if err != nil {
		return result, err
	}
	// the search is added here on the text columns only, useQuery is given
	// the paging and order alone
	query := options.QueryArgs
	search := ""
	if query != nil && query.Search != "" {
		search = query.Search
		copied := *query
		copied.Search = ""
		query = &copied
	}
	db, err = useQuery(db, database.PROGRAM_TABLE, query, allowedProgramColumns...)
	if err != nil {
		return result, errs.Wrap(err)
	}
	if search != "" {
		db = db.Where(programSearch(search))
	}
	var rows []database.Program
	res := withPeek(db, options.QueryArgs).Find(&rows)
	if res.Error != nil {
		return result, errs.Wrap(res.Error)
	}
	hasNextPage := trimPeek(&rows, options.QueryArgs)
	result.HasNextPage = hasNextPage
	for i := range rows {
		p, err := ToProgram(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		if rows[i].Company != nil {
			p.Company = ToCompany(rows[i].Company)
		}
		result.Rows = append(result.Rows, p)
	}
	return result, nil
}

// whereListStatus limits a program query to one of model.ProgramListStatuses
// with the same rules as model.ProgramStatusOf. An active program is split
// into needs attention and active the way the program list shows it, see
// needsAttention. A program being closed or deleted is in none of them. An
// unknown status is a validation error.
func (r *Program) whereListStatus(db *gorm.DB, status string, now time.Time) (*gorm.DB, error) {
	if status == "" {
		return db, nil
	}
	closedAt, pausedAt, startAt := programsColumn("closed_at"), programsColumn("paused_at"), programsColumn("start_at")
	notClosed := clause.Eq{Column: closedAt, Value: nil}
	started := clause.And(
		notClosed,
		clause.Neq{Column: startAt, Value: nil},
		clause.Lte{Column: startAt, Value: now.UTC()},
	)
	running := clause.And(started, clause.Eq{Column: pausedAt, Value: nil})
	// a program being closed or deleted is paused by that, it is neither
	// paused, scheduled nor a draft
	notStopping := clause.And(
		clause.Eq{Column: programsColumn("closing_at"), Value: nil},
		clause.Eq{Column: programsColumn("deleting_at"), Value: nil},
	)
	switch status {
	case model.ProgramStatusClosed:
		return db.Where(clause.Neq{Column: closedAt, Value: nil}), nil
	case model.ProgramStatusDraft:
		return db.Where(notClosed).Where(notStopping).Where(clause.Eq{Column: startAt, Value: nil}), nil
	case model.ProgramStatusScheduled:
		return db.Where(notClosed).Where(notStopping).Where(clause.Gt{Column: startAt, Value: now.UTC()}), nil
	case model.ProgramStatusPaused:
		return db.Where(started).Where(notStopping).Where(clause.Neq{Column: pausedAt, Value: nil}), nil
	case model.ProgramStatusActive:
		return db.Where(running).Where(r.needsNoAttention(now)), nil
	case model.ProgramStatusNeedsAttention:
		return db.Where(running).Where(r.needsAttention(now)), nil
	}
	return db, errs.NewValidationError(errProgramUnknownStatus)
}

// errProgramUnknownStatus is a list status filter that is not one of
// model.ProgramListStatuses
var errProgramUnknownStatus = errors.New("unknown program status")

// brokenStep selects the steps of the program row whose template is missing
// or cannot be sent, or can not send a language the program picks, see
// stepLanguageBroken. It is a subquery on the steps of the program row, read
// through the program index of the steps, instead of a list of every program
// with such a step. A step without a template is not counted, as in
// programAttentionReason.
func (r *Program) brokenStep() *gorm.DB {
	stepTemplate := clause.Column{Table: database.PROGRAM_STEP_TABLE, Name: "campaign_template_id"}
	usable := r.DB.Model(&database.CampaignTemplate{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "id"}, stepTemplate}}).
		Where(clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_TEMPLATE_TABLE, Name: "is_usable"}, Value: true})
	steps := r.DB.Model(&database.ProgramStep{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.PROGRAM_STEP_TABLE, Name: "program_id"}, programsColumn("id")}}).
		Where(clause.Neq{Column: stepTemplate, Value: nil}).
		Where(clause.Or(
			clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{usable}},
			r.stepLanguageBroken(),
		))
	return steps
}

// hasPeople selects the people of the program row who have not exited:
// active, completed or stopped. A started program without one has empty
// groups, as a member is enrolled or made active again on the next membership
// pass and is exited only after leaving the groups.
func (r *Program) hasPeople() *gorm.DB {
	people := r.DB.Model(&database.ProgramEnrollment{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"}, programsColumn("id")}}).
		Where(clause.Neq{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "status"}, Value: model.ProgramEnrollmentExited})
	return people
}

// ProgramStallGrace is how long the next send of an active person may be
// overdue, or a queued send may wait past its send time, before a running
// program counts as stalled. The due times and send times are drawn inside
// the sending days, hours and skip ranges, so a weekend, a holiday or the
// delay of a new member never makes them overdue.
const ProgramStallGrace = 24 * time.Hour

// ProgramStallFailures is how many failed advances in a row of an active
// person make a running program count as stalled. Each failure moves the next
// try further out, see the program scheduler backoff, so the next send of
// such a person is never overdue. Five failures are about a day of tries.
const ProgramStallFailures = 5

// ProgramFailWindow is how far back sends that failed are looked at. A
// program whose sends failed in that time while none went out needs
// attention, as its sender does not work. One failed send among others that
// went out, such as an address that bounced, does not count.
const ProgramFailWindow = 7 * 24 * time.Hour

// ProgramStartGrace is how long a program may run past its start time before
// the membership pass marks it started. A program past it with nobody in it
// has empty groups, see emptyGroups.
const ProgramStartGrace = time.Hour

// enrollmentsOfRow selects the enrollments of the program row with a status
func (r *Program) enrollmentsOfRow(status string) *gorm.DB {
	return r.DB.Model(&database.ProgramEnrollment{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "program_id"}, programsColumn("id")}}).
		Where(clause.Eq{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "status"}, Value: status})
}

// queuedOfRow selects the sends of the hidden campaigns of the program row
// that are neither sent nor cancelled. The predicates are those of the
// partial index idx_campaign_recipients_unsent_campaign, so only the queued
// rows of each campaign are read.
func (r *Program) queuedOfRow() *gorm.DB {
	return r.sendsOfRow().Where(unsentSends())
}

// sendsOfRow selects the sends of the hidden campaigns of the program of the
// outer row
func (r *Program) sendsOfRow() *gorm.DB {
	campaigns := r.DB.Model(&database.Campaign{}).
		Select("id").
		Where(clause.Expr{SQL: "? = ?", Vars: []any{clause.Column{Table: database.CAMPAIGN_TABLE, Name: "program_id"}, programsColumn("id")}})
	return r.DB.Model(&database.CampaignRecipient{}).
		Select("id").
		Where(clause.Expr{SQL: "? IN (?)", Vars: []any{clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "campaign_id"}, campaigns}})
}

// columnAfter matches a row whose time column is after the given time. Times
// are stored in more than one text layout, so they are compared as julian
// days.
func columnAfter(name string, after time.Time) clause.Expression {
	return clause.Expr{
		SQL: "julianday(?) > julianday(?)",
		Vars: []any{
			clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: name},
			after.UTC().Format(sqliteCompareLayout),
		},
	}
}

// notTried matches a send the sender has not tried yet. A send that failed
// keeps its attempt time and is never tried again.
func notTried() clause.Expression {
	return clause.Eq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "last_attempt_at"}, Value: nil}
}

// queuedBefore matches a queued send whose send time is before the given
// time. The send time is stored in more than one text layout, so it is
// compared as a julian day.
func queuedBefore(before time.Time) clause.Expression {
	return clause.Expr{
		SQL: "julianday(?) < julianday(?)",
		Vars: []any{
			clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "send_at"},
			before.UTC().Format(sqliteCompareLayout),
		},
	}
}

// stalled matches the started programs whose sends do not go out: a queued
// send not tried yet waits more than ProgramStallGrace past its send time,
// sends failed within ProgramFailWindow while none went out, also when nobody
// is active any more, the next send of an active person is more than
// ProgramStallGrace overdue, the advance of an active person failed
// ProgramStallFailures times in a row, or an active person has no next send
// while nothing is queued. It is the same rule as programStalledReason. The
// due times are read through the program, status and due index.
func (r *Program) stalled(now time.Time) clause.Expression {
	limit := now.Add(-ProgramStallGrace)
	nextDue := clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "next_due_at"}
	overdue := r.enrollmentsOfRow(model.ProgramEnrollmentActive).
		Where(clause.Lt{Column: nextDue, Value: limit.UTC()})
	failing := r.enrollmentsOfRow(model.ProgramEnrollmentActive).
		Where(clause.Gte{Column: clause.Column{Table: database.PROGRAM_ENROLLMENT_TABLE, Name: "advance_failures"}, Value: ProgramStallFailures})
	unplanned := r.enrollmentsOfRow(model.ProgramEnrollmentActive).
		Where(clause.Eq{Column: nextDue, Value: nil})
	failSince := now.Add(-ProgramFailWindow)
	failed := r.queuedOfRow().
		Where(clause.Neq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "last_attempt_at"}, Value: nil}).
		Where(columnAfter("last_attempt_at", failSince))
	sent := r.sendsOfRow().
		Where(clause.Neq{Column: clause.Column{Table: database.CAMPAIGN_RECIPIENT_TABLE_NAME, Name: "sent_at"}, Value: nil}).
		Where(columnAfter("sent_at", failSince))
	return clause.And(
		clause.Neq{Column: programsColumn("started_at"), Value: nil},
		clause.Or(
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{r.queuedOfRow().Where(notTried()).Where(queuedBefore(limit))}},
			clause.And(
				clause.Expr{SQL: "EXISTS (?)", Vars: []any{failed}},
				clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{sent}},
			),
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{overdue}},
			clause.Expr{SQL: "EXISTS (?)", Vars: []any{failing}},
			clause.And(
				clause.Expr{SQL: "EXISTS (?)", Vars: []any{unplanned}},
				clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{r.queuedOfRow().Where(notTried())}},
			),
		),
	)
}

// emptyGroups matches the programs that run with nobody in them, see
// hasPeople: marked started, or past their start by more than
// ProgramStartGrace. It is the same rule as programAttentionReason.
func (r *Program) emptyGroups(now time.Time) clause.Expression {
	return clause.And(
		clause.Or(
			clause.Neq{Column: programsColumn("started_at"), Value: nil},
			clause.Lte{Column: programsColumn("start_at"), Value: now.Add(-ProgramStartGrace).UTC()},
		),
		clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{r.hasPeople()}},
	)
}

// notEmptyGroups matches the programs emptyGroups does not. The start time
// may be empty, so each case is written out instead of negating emptyGroups.
func (r *Program) notEmptyGroups(now time.Time) clause.Expression {
	return clause.Or(
		clause.And(
			clause.Eq{Column: programsColumn("started_at"), Value: nil},
			clause.Or(
				clause.Eq{Column: programsColumn("start_at"), Value: nil},
				clause.Gt{Column: programsColumn("start_at"), Value: now.Add(-ProgramStartGrace).UTC()},
			),
		),
		clause.Expr{SQL: "EXISTS (?)", Vars: []any{r.hasPeople()}},
	)
}

// sqliteCompareLayout is a time layout the SQLite date functions read
const sqliteCompareLayout = "2006-01-02 15:04:05"

// needsAttention matches the programs with a step whose template is missing or
// cannot be sent, the ones whose groups are empty, see emptyGroups, and the
// stalled ones, see stalled. It is the same rule as programAttentionReason.
func (r *Program) needsAttention(now time.Time) clause.Expression {
	return clause.Or(
		clause.Expr{SQL: "EXISTS (?)", Vars: []any{r.brokenStep()}},
		r.emptyGroups(now),
		r.stalled(now),
	)
}

// needsNoAttention matches the programs needsAttention does not
func (r *Program) needsNoAttention(now time.Time) clause.Expression {
	return clause.And(
		clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{r.brokenStep()}},
		r.notEmptyGroups(now),
		clause.Expr{SQL: "NOT (?)", Vars: []any{r.stalled(now)}},
	)
}

// GetActivePrograms returns programs that have started and are neither paused
// nor closed, each with its steps and assigned group ids loaded, for the
// scheduler. Steps and groups are read in batches over all programs.
func (r *Program) GetActivePrograms(ctx context.Context, now time.Time) ([]*model.Program, error) {
	var rows []database.Program
	// a program past its end date is closed in the background, until then no
	// one joins or leaves it
	res := r.DB.WithContext(ctx).
		Where(r.runningPrograms(now)).
		Where(clause.Or(
			clause.Eq{Column: clause.Column{Name: "end_at"}, Value: nil},
			clause.Gt{Column: clause.Column{Name: "end_at"}, Value: now.UTC()},
		)).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	programs := make([]*model.Program, 0, len(rows))
	byID := map[uuid.UUID]*model.Program{}
	ids := make([]*uuid.UUID, 0, len(rows))
	for i := range rows {
		p, err := ToProgram(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		p.Steps = []*model.ProgramStep{}
		p.RecipientGroupIDs = nullable.NewNullableWithValue([]*uuid.UUID{})
		programs = append(programs, p)
		byID[*rows[i].ID] = p
		ids = append(ids, rows[i].ID)
	}
	for start := 0; start < len(ids); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(ids))
		inPrograms := clause.IN{Column: clause.Column{Name: "program_id"}, Values: uuidValues(ids[start:end])}
		var steps []database.ProgramStep
		if err := r.DB.WithContext(ctx).
			Where(inPrograms).
			Order(clause.OrderByColumn{Column: clause.Column{Name: "position"}}).
			Find(&steps).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		for i := range steps {
			if p := byID[*steps[i].ProgramID]; p != nil {
				p.Steps = append(p.Steps, ToProgramStep(&steps[i]))
			}
		}
		var groups []database.ProgramRecipientGroup
		if err := r.DB.WithContext(ctx).Where(inPrograms).Find(&groups).Error; err != nil {
			return nil, errs.Wrap(err)
		}
		for i := range groups {
			if p := byID[*groups[i].ProgramID]; p != nil {
				p.RecipientGroupIDs = nullable.NewNullableWithValue(append(p.RecipientGroupIDs.MustGet(), groups[i].RecipientGroupID))
			}
		}
	}
	return programs, nil
}

// ReplaceSteps makes the program's steps match the given list in one
// transaction, see replaceStepsTx.
func (r *Program) ReplaceSteps(
	ctx context.Context,
	programID *uuid.UUID,
	steps []*model.ProgramStep,
) error {
	return r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return replaceStepsTx(tx, programID, steps)
	})
}

// replaceStepsTx makes the program's steps match the given list. A step that
// carries the id of an existing step keeps that id, so the hidden campaigns and
// the delivery history linked to it stay valid. Steps without a known id are
// added and steps left out are removed.
func replaceStepsTx(tx *gorm.DB, programID *uuid.UUID, steps []*model.ProgramStep) error {
	byProgram := programIDEq(clause.Column{Name: "program_id"}, programID)
	// move every position out of the way first, the position index is unique.
	// Positions are never negative, so -1 - position gives each a free place.
	// It is the first statement, so the transaction writes before it reads
	if err := tx.Model(&database.ProgramStep{}).
		Where(byProgram).
		Update("position", gorm.Expr("? - ?", -1, clause.Column{Name: "position"})).Error; err != nil {
		return errs.Wrap(err)
	}
	existing := []database.ProgramStep{}
	if err := tx.Where(byProgram).Find(&existing).Error; err != nil {
		return errs.Wrap(err)
	}
	// a step keeps its id only while it keeps its template. A new template is
	// a new step, so its sends go into a new hidden campaign
	knownTemplate := map[uuid.UUID]uuid.UUID{}
	for _, e := range existing {
		knownTemplate[*e.ID] = *e.CampaignTemplateID
	}
	kept := map[uuid.UUID]bool{}
	now := utils.NowRFC3339UTC()
	for i, s := range steps {
		templateID := s.CampaignTemplateID.MustGet()
		position := i
		if s.Position.IsSpecified() && !s.Position.IsNull() {
			position = s.Position.MustGet()
		}
		sameStep := false
		if s.ID.IsSpecified() && !s.ID.IsNull() {
			t, ok := knownTemplate[s.ID.MustGet()]
			sameStep = ok && t == templateID && !kept[s.ID.MustGet()]
		}
		if sameStep {
			id := s.ID.MustGet()
			kept[id] = true
			if err := tx.Model(&database.ProgramStep{}).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
				Updates(map[string]any{
					"position":   position,
					"updated_at": now,
				}).Error; err != nil {
				return errs.Wrap(err)
			}
			continue
		}
		if err := tx.Model(&database.ProgramStep{}).Create(map[string]any{
			"id":                   uuid.New(),
			"created_at":           now,
			"updated_at":           now,
			"program_id":           programID,
			"campaign_template_id": templateID,
			"position":             position,
		}).Error; err != nil {
			return errs.Wrap(err)
		}
	}
	removed := []*uuid.UUID{}
	for _, e := range existing {
		if !kept[*e.ID] {
			removed = append(removed, e.ID)
		}
	}
	for start := 0; start < len(removed); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(removed))
		if err := tx.Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(removed[start:end])}).
			Delete(&database.ProgramStep{}).Error; err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

// GetSteps returns a program's steps ordered by position.
func (r *Program) GetSteps(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*model.ProgramStep, error) {
	var rows []database.ProgramStep
	res := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "position"}}).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	steps := make([]*model.ProgramStep, 0, len(rows))
	for i := range rows {
		steps = append(steps, ToProgramStep(&rows[i]))
	}
	return steps, nil
}

// replaceRecipientGroupsTx deletes a program's assigned groups and inserts the
// given ones
func replaceRecipientGroupsTx(tx *gorm.DB, programID *uuid.UUID, groupIDs []*uuid.UUID) error {
	if err := tx.Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Delete(&database.ProgramRecipientGroup{}).Error; err != nil {
		return errs.Wrap(err)
	}
	rows := []database.ProgramRecipientGroup{}
	seen := map[uuid.UUID]bool{}
	for _, gid := range groupIDs {
		if gid == nil || seen[*gid] {
			continue
		}
		seen[*gid] = true
		rows = append(rows, database.ProgramRecipientGroup{ProgramID: programID, RecipientGroupID: gid})
	}
	if len(rows) == 0 {
		return nil
	}
	return errs.Wrap(tx.Create(&rows).Error)
}

// GetRecipientGroupIDs returns the ids of a program's assigned groups.
func (r *Program) GetRecipientGroupIDs(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*uuid.UUID, error) {
	var rows []database.ProgramRecipientGroup
	res := r.DB.WithContext(ctx).
		Where(programIDEq(clause.Column{Name: "program_id"}, programID)).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	ids := make([]*uuid.UUID, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].RecipientGroupID)
	}
	return ids, nil
}

// getRecipientGroups returns the groups assigned to a program
func (r *Program) getRecipientGroups(
	ctx context.Context,
	programID *uuid.UUID,
) ([]*model.RecipientGroup, error) {
	assigned := r.DB.Model(&database.ProgramRecipientGroup{}).
		Select("recipient_group_id").
		Where(programIDEq(clause.Column{Name: "program_id"}, programID))
	var rows []database.RecipientGroup
	res := r.DB.WithContext(ctx).
		Where("id IN (?)", assigned).
		Find(&rows)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	groups := make([]*model.RecipientGroup, 0, len(rows))
	for i := range rows {
		g, err := ToRecipientGroup(&rows[i])
		if err != nil {
			return nil, errs.Wrap(err)
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// openPrograms selects the ids of the programs that are not closed. A closed
// program keeps its history but no longer holds on to its groups or
// templates.
func (r *Program) openPrograms() *gorm.DB {
	return r.DB.Model(&database.Program{}).
		Select("id").
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil})
}

// GetNamesByRecipientGroupID returns the names of the programs that are not
// closed and have the recipient group assigned
func (r *Program) GetNamesByRecipientGroupID(
	ctx context.Context,
	groupID *uuid.UUID,
) ([]string, error) {
	names := []string{}
	assigned := r.DB.Model(&database.ProgramRecipientGroup{}).
		Select("program_id").
		Where(clause.Eq{Column: clause.Column{Name: "recipient_group_id"}, Value: groupID})
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where("id IN (?)", assigned).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "name"}}).
		Pluck("name", &names)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return names, nil
}

// RemoveRecipientGroupFromClosedPrograms removes a recipient group from every
// closed program, used when the group is deleted. Programs that are not closed
// block the delete, see GetNamesByRecipientGroupID.
func (r *Program) RemoveRecipientGroupFromClosedPrograms(
	ctx context.Context,
	groupID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "recipient_group_id"}, Value: groupID}).
		Where("program_id NOT IN (?)", r.openPrograms()).
		Delete(&database.ProgramRecipientGroup{})
	return errs.Wrap(res.Error)
}

// GetIDsByCompanyID returns the ids of the programs that belong to a company
func (r *Program) GetIDsByCompanyID(
	ctx context.Context,
	companyID *uuid.UUID,
) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: companyColumn(database.PROGRAM_TABLE), Value: companyID}), "id")
}

// GetNamesByCampaignTemplateID returns the names of the programs that are not
// closed and have a step using the campaign template
func (r *Program) GetNamesByCampaignTemplateID(
	ctx context.Context,
	templateID *uuid.UUID,
) ([]string, error) {
	names := []string{}
	stepsUsing := r.DB.Model(&database.ProgramStep{}).
		Select("program_id").
		Where(clause.Eq{Column: clause.Column{Name: "campaign_template_id"}, Value: templateID})
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where("id IN (?)", stepsUsing).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "name"}}).
		Pluck("name", &names)
	if res.Error != nil {
		return nil, errs.Wrap(res.Error)
	}
	return names, nil
}

// RemoveCampaignTemplateFromClosedPrograms removes the steps of closed
// programs that use a campaign template, used when the template is deleted.
// The sends and results of those steps stay, they name the template as
// deleted. Programs that are not closed block the delete, see
// GetNamesByCampaignTemplateID.
func (r *Program) RemoveCampaignTemplateFromClosedPrograms(
	ctx context.Context,
	templateID *uuid.UUID,
) error {
	res := r.DB.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "campaign_template_id"}, Value: templateID}).
		Where("program_id NOT IN (?)", r.openPrograms()).
		Delete(&database.ProgramStep{})
	return errs.Wrap(res.Error)
}

// ProgramListSummary is what the program list shows for one program
type ProgramListSummary struct {
	// Enrolled counts the active people
	Enrolled int64
	// Stopped counts the people still active when the program closed
	Stopped int64
	// People counts the people who have not exited: active, completed or
	// stopped
	People int64
	// Total counts every enrollment, also the people who were removed
	Total int64
	// NextDueAt is the earliest next send of the active people
	NextDueAt *time.Time
	// Unplanned counts the active people without a next send
	Unplanned int64
	// Failing counts the active people whose advance failed at least
	// ProgramStallFailures times in a row
	Failing int64
	// Queued counts the sends of the hidden campaigns that are neither sent
	// nor cancelled, and OldestQueuedAt is the earliest send time of the ones
	// the sender has not tried yet
	Queued         int64
	OldestQueuedAt *time.Time
	// NewestFailedAt is the newest attempt of a send that failed, and
	// NewestSentAt the newest send that went out
	NewestFailedAt  *time.Time
	NewestSentAt    *time.Time
	StepTemplateIDs []uuid.UUID
	// Simulations counts the steps
	Simulations int
	// BrokenLanguage is set when the program picks a language by a field that
	// one of its step templates can not send, see stepLanguageBroken
	BrokenLanguage bool
}

// sqliteTimeLayouts are the layouts SQLite returns a time in when it comes
// from an aggregate, which has no declared column type to convert from
var sqliteTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
}

// julianToTime converts a julian day of SQLite to a time in UTC
func julianToTime(day float64) time.Time {
	const unixEpochJulian = 2440587.5
	return time.UnixMilli(int64((day - unixEpochJulian) * 86400000)).UTC()
}

func parseSQLiteTime(v string) (time.Time, bool) {
	for _, layout := range sqliteTimeLayouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// GetListSummaries returns the active and the not exited enrollment counts,
// the earliest next send, the people without one, the queued sends and the
// step templates of each given program in a few queries in total.
func (r *Program) GetListSummaries(
	ctx context.Context,
	programIDs []*uuid.UUID,
) (map[uuid.UUID]*ProgramListSummary, error) {
	out := map[uuid.UUID]*ProgramListSummary{}
	if len(programIDs) == 0 {
		return out, nil
	}
	for _, id := range programIDs {
		if id != nil {
			out[*id] = &ProgramListSummary{}
		}
	}
	inPrograms := clause.IN{Column: clause.Column{Name: "program_id"}, Values: uuidValues(programIDs)}
	isActive := clause.Eq{Column: clause.Column{Name: "status"}, Value: model.ProgramEnrollmentActive}

	var counts []struct {
		ProgramID uuid.UUID
		Status    string
		People    int64
	}
	err := r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("program_id, status, COUNT(*) AS people").
		Where(inPrograms).
		Group("program_id, status").
		Scan(&counts).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, c := range counts {
		row, ok := out[c.ProgramID]
		if !ok {
			continue
		}
		if c.Status == model.ProgramEnrollmentActive {
			row.Enrolled += c.People
		}
		if c.Status == model.ProgramEnrollmentStopped {
			row.Stopped += c.People
		}
		if c.Status != model.ProgramEnrollmentExited {
			row.People += c.People
		}
		row.Total += c.People
	}
	// the people of a program whose Anonymize All began are counted from the
	// counts stored when it began, its enrollments are removed
	if err := r.applyStoredPeopleCounts(ctx, programIDs, out); err != nil {
		return nil, err
	}

	var nexts []struct {
		ProgramID uuid.UUID
		NextDueAt *string
		Unplanned int64
		Failing   int64
	}
	// the earliest due time, the people without one and the people whose
	// advance keeps failing, read through the program, status and due index
	nextDue := clause.Column{Name: "next_due_at"}
	failures := clause.Column{Name: "advance_failures"}
	err = r.DB.WithContext(ctx).
		Model(&database.ProgramEnrollment{}).
		Select("program_id, MIN(?) AS next_due_at, COUNT(*) - COUNT(?) AS unplanned, SUM(CASE WHEN ? >= ? THEN 1 ELSE 0 END) AS failing",
			nextDue, nextDue, failures, ProgramStallFailures).
		Where(inPrograms).
		Where(isActive).
		Group("program_id").
		Scan(&nexts).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, n := range nexts {
		row, ok := out[n.ProgramID]
		if !ok {
			continue
		}
		row.Unplanned = n.Unplanned
		row.Failing = n.Failing
		if n.NextDueAt == nil {
			continue
		}
		if t, ok := parseSQLiteTime(*n.NextDueAt); ok {
			row.NextDueAt = &t
		}
	}

	// the hidden campaigns of the programs, then the queued sends of each
	// campaign through the partial index of unsent rows
	var campaigns []struct {
		ID        uuid.UUID
		ProgramID uuid.UUID
	}
	err = r.DB.WithContext(ctx).
		Model(&database.Campaign{}).
		Select("id", "program_id").
		Where(inPrograms).
		Scan(&campaigns).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	programOf := make(map[uuid.UUID]uuid.UUID, len(campaigns))
	campaignIDs := make([]*uuid.UUID, 0, len(campaigns))
	for i := range campaigns {
		programOf[campaigns[i].ID] = campaigns[i].ProgramID
		campaignIDs = append(campaignIDs, &campaigns[i].ID)
	}
	// the send time is stored in more than one text layout, so the earliest
	// is taken as a julian day
	for start := 0; start < len(campaignIDs); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(campaignIDs))
		var queued []struct {
			CampaignID   uuid.UUID
			Queued       int64
			OldestJulian *float64
			FailedJulian *float64
		}
		err = r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select(
				"campaign_id, COUNT(*) AS queued, MIN(CASE WHEN ? IS NULL THEN julianday(?) END) AS oldest_julian, MAX(julianday(?)) AS failed_julian",
				clause.Column{Name: "last_attempt_at"}, clause.Column{Name: "send_at"}, clause.Column{Name: "last_attempt_at"},
			).
			Where(unsentSends()).
			Where(clause.IN{Column: clause.Column{Name: "campaign_id"}, Values: uuidValues(campaignIDs[start:end])}).
			Group("campaign_id").
			Scan(&queued).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, q := range queued {
			row, ok := out[programOf[q.CampaignID]]
			if !ok {
				continue
			}
			row.Queued += q.Queued
			if q.FailedJulian != nil {
				t := julianToTime(*q.FailedJulian)
				if row.NewestFailedAt == nil || t.After(*row.NewestFailedAt) {
					row.NewestFailedAt = &t
				}
			}
			if q.OldestJulian == nil {
				continue
			}
			t := julianToTime(*q.OldestJulian)
			if row.OldestQueuedAt == nil || t.Before(*row.OldestQueuedAt) {
				row.OldestQueuedAt = &t
			}
		}
		var sent []struct {
			CampaignID uuid.UUID
			SentJulian *float64
		}
		err = r.DB.WithContext(ctx).
			Model(&database.CampaignRecipient{}).
			Select("campaign_id, MAX(julianday(?)) AS sent_julian", clause.Column{Name: "sent_at"}).
			Where(clause.Neq{Column: clause.Column{Name: "sent_at"}, Value: nil}).
			Where(clause.IN{Column: clause.Column{Name: "campaign_id"}, Values: uuidValues(campaignIDs[start:end])}).
			Group("campaign_id").
			Scan(&sent).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, q := range sent {
			row, ok := out[programOf[q.CampaignID]]
			if !ok || q.SentJulian == nil {
				continue
			}
			t := julianToTime(*q.SentJulian)
			if row.NewestSentAt == nil || t.After(*row.NewestSentAt) {
				row.NewestSentAt = &t
			}
		}
	}

	var steps []database.ProgramStep
	err = r.DB.WithContext(ctx).
		Where(inPrograms).
		Find(&steps).Error
	if err != nil {
		return nil, errs.Wrap(err)
	}
	for _, st := range steps {
		row, ok := out[*st.ProgramID]
		if !ok {
			continue
		}
		if st.CampaignTemplateID != nil {
			row.StepTemplateIDs = append(row.StepTemplateIDs, *st.CampaignTemplateID)
		}
		row.Simulations++
	}
	broken, err := r.GetProgramsWithBrokenLanguage(ctx, programIDs)
	if err != nil {
		return nil, err
	}
	for id := range broken {
		if row, ok := out[id]; ok {
			row.BrokenLanguage = true
		}
	}
	return out, nil
}

// GetTemplateUsability returns for each existing template id whether it can be
// sent. A template that is missing from the result no longer exists. The ids
// are deduped and read in batches, so a long list stays under the bind
// parameter limit.
func (r *Program) GetTemplateUsability(
	ctx context.Context,
	templateIDs []uuid.UUID,
) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	seen := map[uuid.UUID]bool{}
	ids := []*uuid.UUID{}
	for i := range templateIDs {
		if seen[templateIDs[i]] {
			continue
		}
		seen[templateIDs[i]] = true
		ids = append(ids, &templateIDs[i])
	}
	for start := 0; start < len(ids); start += programEnrollmentBatch {
		end := min(start+programEnrollmentBatch, len(ids))
		var rows []database.CampaignTemplate
		err := r.DB.WithContext(ctx).
			Select("id", "is_usable").
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uuidValues(ids[start:end])}).
			Find(&rows).Error
		if err != nil {
			return nil, errs.Wrap(err)
		}
		for _, t := range rows {
			out[*t.ID] = t.IsUsable
		}
	}
	return out, nil
}

// programNotAnonymizing is the condition that an Anonymize All of the
// program in table is not running: it never began or it finished. While it
// runs the campaigns it has not reached still name people, so per person data
// of such a program is left out, the same as once it began.
func programNotAnonymizing(table string) clause.Expression {
	return clause.Or(
		clause.Eq{Column: clause.Column{Table: table, Name: "anonymizing_at"}, Value: nil},
		clause.Neq{Column: clause.Column{Table: table, Name: "anonymized_at"}, Value: nil},
	)
}

// programIDEq is the condition that column holds the program id. A nil id
// matches no row: an equality with nil is IS NULL, which would match every
// row that belongs to no program, such as every normal campaign.
func programIDEq(column clause.Column, id *uuid.UUID) clause.Expression {
	if id == nil {
		return clause.Expr{SQL: "1 = 0"}
	}
	return clause.Eq{Column: column, Value: id}
}

// runningPrograms is the condition for a program the scheduler runs: its start
// time has passed, it is neither paused nor closed and its company is not
// being deleted. A draft has no start.
func (r *Program) runningPrograms(now time.Time) clause.Expression {
	deleting := r.DB.Model(&database.Company{}).
		Select("id").
		Where(clause.Neq{Column: clause.Column{Table: database.COMPANY_TABLE, Name: "deleting_at"}, Value: nil})
	return clause.And(
		clause.Eq{Column: clause.Column{Name: "paused_at"}, Value: nil},
		clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil},
		clause.Neq{Column: clause.Column{Name: "start_at"}, Value: nil},
		clause.Lte{Column: clause.Column{Name: "start_at"}, Value: now.UTC()},
		clause.Or(
			clause.Eq{Column: clause.Column{Name: "company_id"}, Value: nil},
			clause.Expr{SQL: "? NOT IN (?)", Vars: []any{clause.Column{Name: "company_id"}, deleting}},
		),
	)
}

// GetIDsToClose returns the ids of the programs the scheduler closes: not
// closed and not being deleted, and either past their end date or with a close
// that began and did not finish
func (r *Program) GetIDsToClose(ctx context.Context, now time.Time, limit int, skip []*uuid.UUID) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Where(clause.Or(
			clause.Neq{Column: clause.Column{Name: "closing_at"}, Value: nil},
			clause.And(
				clause.Neq{Column: clause.Column{Name: "end_at"}, Value: nil},
				clause.Lte{Column: clause.Column{Name: "end_at"}, Value: now.UTC()},
			),
		)).
		Where(notInIDs(skip)).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "created_at"}}).
		Limit(limit), "id")
}

// IsEndedByID reports whether a program has an end date that is not after
// now. It reads one row by its primary key. A program that is gone counts as
// ended, so its hidden campaigns send nothing more.
func (r *Program) IsEndedByID(ctx context.Context, id *uuid.UUID, now time.Time) (bool, error) {
	rows := []database.Program{}
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Select("end_at").
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Limit(1).
		Find(&rows)
	if res.Error != nil {
		return false, errs.Wrap(res.Error)
	}
	if len(rows) == 0 {
		return true, nil
	}
	end := rows[0].EndAt
	return end != nil && !now.Before(*end), nil
}

// uuidValues turns ids into values for an IN clause, skipping nil ones
func uuidValues(ids []*uuid.UUID) []any {
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		if id != nil {
			values = append(values, *id)
		}
	}
	return values
}

// pluckUUIDs reads one uuid column of a query
func pluckUUIDs(db *gorm.DB, column string) ([]*uuid.UUID, error) {
	ids := []uuid.UUID{}
	if err := db.Pluck(column, &ids).Error; err != nil {
		return nil, errs.Wrap(err)
	}
	out := make([]*uuid.UUID, 0, len(ids))
	for i := range ids {
		out = append(out, &ids[i])
	}
	return out, nil
}

// ToProgram maps a program row to the model, without steps or groups.
func ToProgram(row *database.Program) (*model.Program, error) {
	id := nullable.NewNullableWithValue(*row.ID)
	name := nullable.NewNullableWithValue(*vo.NewString64Must(row.Name))
	companyID := nullable.NewNullNullable[uuid.UUID]()
	if row.CompanyID != nil {
		companyID.Set(*row.CompanyID)
	} else {
		companyID.SetNull()
	}
	closedAt := nullable.NewNullNullable[time.Time]()
	if row.ClosedAt != nil {
		closedAt.Set(*row.ClosedAt)
	} else {
		closedAt.SetNull()
	}
	pausedAt := nullable.NewNullNullable[time.Time]()
	if row.PausedAt != nil {
		pausedAt.Set(*row.PausedAt)
	} else {
		pausedAt.SetNull()
	}
	weekDays := nullable.NewNullNullable[vo.CampaignWeekDays]()
	if row.ConstraintWeekDays != nil {
		weekDays.Set(*vo.NewCampaignWeekDaysMust(*row.ConstraintWeekDays))
	} else {
		weekDays.SetNull()
	}
	startTime := nullable.NewNullNullable[vo.CampaignTimeConstraint]()
	if row.ConstraintStartTime != nil {
		startTime.Set(*vo.NewCampaignTimeConstraintMust(*row.ConstraintStartTime))
	} else {
		startTime.SetNull()
	}
	endTime := nullable.NewNullNullable[vo.CampaignTimeConstraint]()
	if row.ConstraintEndTime != nil {
		endTime.Set(*vo.NewCampaignTimeConstraintMust(*row.ConstraintEndTime))
	} else {
		endTime.SetNull()
	}
	startAt := nullable.NewNullNullable[time.Time]()
	if row.StartAt != nil {
		startAt.Set(*row.StartAt)
	} else {
		startAt.SetNull()
	}
	program := &model.Program{
		ID:                    id,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
		Name:                  name,
		CompanyID:             companyID,
		ClosedAt:              closedAt,
		PausedAt:              pausedAt,
		OrderMode:             nullable.NewNullableWithValue(row.OrderMode),
		LoopMode:              nullable.NewNullableWithValue(row.LoopMode),
		TemplateMixBy:         nullable.NewNullableWithValue(row.TemplateMixBy),
		LanguageRouteBy:       nullable.NewNullableWithValue(row.LanguageRouteBy),
		LanguageDefault:       nullable.NewNullableWithValue(row.LanguageDefault),
		StepIntervalMinutes:   nullable.NewNullableWithValue(row.StepIntervalMinutes),
		SpreadMinutes:         nullable.NewNullableWithValue(row.SpreadMinutes),
		NewMemberDelayMinutes: nullable.NewNullableWithValue(row.NewMemberDelayMinutes),
		TimeZone:              nullable.NewNullableWithValue(row.TimeZone),
		StartedAt:             row.StartedAt,
		ClosingAt:             row.ClosingAt,
		DeletingAt:            row.DeletingAt,
		ReplanPendingAt:       row.ReplanPendingAt,
		ReplanOldEndAt:        row.ReplanOldEndAt,
		ConstraintWeekDays:    weekDays,
		ConstraintStartTime:   startTime,
		ConstraintEndTime:     endTime,
		IsAnonymous:           nullable.NewNullableWithValue(row.IsAnonymous),
		SaveSubmittedData:     nullable.NewNullableWithValue(row.SaveSubmittedData),
	}
	program.StartAt = startAt
	endAt := nullable.NewNullNullable[time.Time]()
	if row.EndAt != nil {
		endAt.Set(*row.EndAt)
	}
	program.EndAt = endAt
	anonymizeAt := nullable.NewNullNullable[time.Time]()
	if row.AnonymizeAt != nil {
		anonymizeAt.Set(*row.AnonymizeAt)
	}
	program.AnonymizeAt = anonymizeAt
	dataAnonymizeAt := nullable.NewNullNullable[time.Time]()
	if row.DataAnonymizeAt != nil {
		dataAnonymizeAt.Set(*row.DataAnonymizeAt)
	}
	program.DataAnonymizeAt = dataAnonymizeAt
	program.AnonymizedAt = row.AnonymizedAt
	program.AnonymizingAt = row.AnonymizingAt
	program.DataAnonymizedAt = row.DataAnonymizedAt
	program.DataAnonymizingAt = row.DataAnonymizingAt
	program.EndedAutomatically = row.ClosedAt != nil && row.EndAt != nil && !row.ClosedAt.Before(*row.EndAt)
	program.BlackoutDates = nullable.NewNullableWithValue(model.ParseProgramBlackouts(row.BlackoutDates))
	applyProgramSettingsRow(program, row)
	applyProgramReportRow(program, row)
	return program, nil
}

// ToProgramStep maps a step row to the model.
func ToProgramStep(row *database.ProgramStep) *model.ProgramStep {
	step := &model.ProgramStep{
		ID:                 nullable.NewNullableWithValue(*row.ID),
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		ProgramID:          nullable.NewNullableWithValue(*row.ProgramID),
		CampaignTemplateID: nullable.NewNullableWithValue(*row.CampaignTemplateID),
		Position:           nullable.NewNullableWithValue(row.Position),
	}
	return step
}

// GetIDsWithReplanPending returns up to limit programs, not closed nor being
// closed or deleted, with an edit whose people are not all planned again
// yet, the oldest mark first
func (r *Program) GetIDsWithReplanPending(ctx context.Context, limit int) ([]*uuid.UUID, error) {
	return pluckUUIDs(r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Neq{Column: clause.Column{Name: "replan_pending_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "closed_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "closing_at"}, Value: nil}).
		Where(clause.Eq{Column: clause.Column{Name: "deleting_at"}, Value: nil}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "replan_pending_at"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Limit(limit), "id")
}

// ClearReplanPending clears the replan mark of a program and the old end
// with it, only while the mark is still the one given, so a newer mark is
// kept. The updated time is left as it is, as the program itself does not
// change.
func (r *Program) ClearReplanPending(ctx context.Context, id *uuid.UUID, mark time.Time) error {
	res := r.DB.WithContext(ctx).
		Model(&database.Program{}).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Where(clause.Eq{Column: clause.Column{Name: "replan_pending_at"}, Value: mark.UTC()}).
		UpdateColumns(map[string]any{
			"replan_pending_at": nil,
			"replan_old_end_at": nil,
		})
	return errs.Wrap(res.Error)
}
