package service

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/utils"
)

func csvTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func csvCount(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// programName returns the name of a program
func programName(program *model.Program) string {
	if v, err := program.Name.Get(); err == nil {
		return v.String()
	}
	return "program"
}

// ProgramCSVExport is a checked program CSV export, ready to be written
type ProgramCSVExport struct {
	// Name is the program name, for the file name
	Name string
	run  func(ctx context.Context, write func([]string) error) error
}

// Write writes every row of the export in order, each cell made safe for
// spreadsheets, and audits the export once it is written
func (e *ProgramCSVExport) Write(ctx context.Context, write func([]string) error) error {
	return e.run(ctx, write)
}

// ExportCSV checks the session and returns the program results export. A
// normal program gives one row per sent template, written in batches so a
// large program is never held in memory. An anonymous program gives the result
// totals and the month and template rows of its whole settled months only,
// with small groups withheld.
func (s *Program) ExportCSV(
	ctx context.Context,
	session *model.Session,
	id *uuid.UUID,
) (_ *ProgramCSVExport, err error) {
	ae := NewAuditEvent("Program.Export", session)
	defer s.auditDone(ae, &err)
	ae.Details["id"] = id.String()
	program, err := s.authorizedProgram(ctx, session, id, ae, &repository.ProgramOption{WithSteps: true})
	if err != nil {
		return nil, err
	}
	anonymous := program.IsAnonymousOn()
	run := func(ctx context.Context, write func([]string) error) (err error) {
		defer s.auditDone(ae, &err)
		rows := 0
		counted := func(row []string) error {
			rows++
			return write(row)
		}
		if anonymous {
			results, err := s.programResults(ctx, session, program, time.Now(), nil, nil)
			if err != nil {
				return err
			}
			for _, row := range programAggregateCSV(results) {
				if err := counted(row); err != nil {
					return errs.Wrap(err)
				}
			}
		} else if err := s.programSendCSV(ctx, program, counted); err != nil {
			return err
		}
		ae.Details["anonymous"] = anonymous
		ae.Details["rows"] = rows - 1
		s.AuditLogAuthorized(ae)
		return nil
	}
	return &ProgramCSVExport{Name: programName(program), run: run}, nil
}

// programSummaryLines names the result tiles of the program page with their
// values, the phishing counts only when the program has phishing and the
// training counts only when it has training. An anonymous program never
// counts repeat offenders, so it has no such line.
func programSummaryLines(sum *ProgramSummary, anonymous bool) []struct {
	Key, Label string
	Value      *int
} {
	type line = struct {
		Key, Label string
		Value      *int
	}
	out := []line{
		{"recipients", "Recipients", intPtr(sum.Recipients)},
	}
	if sum.HasPhishing {
		out = append(out,
			line{"sent", "Sent", sum.Sent},
			line{"clickedLink", "Clicked link", sum.ClickedLink},
			line{"submittedData", "Data submitted", sum.SubmittedData},
			line{"reported", "Reported", sum.Reported},
		)
	}
	if sum.HasTraining {
		out = append(out,
			line{"trainingSent", "Training sent", sum.TrainingSent},
			line{"trainingStarted", "Training started", sum.TrainingStarted},
			line{"trainingCompleted", "Training completed", sum.TrainingCompleted},
		)
	}
	if anonymous || !sum.HasPhishing {
		return out
	}
	return append(out, line{"repeatOffenders", "Repeat offenders", sum.RepeatOffenders})
}

// programAggregateCSV writes the result totals and the month and template rows
// of an anonymous program. Recipients counts people, every other result total
// and the month and template rows count sends.
func programAggregateCSV(results *ProgramResults) [][]string {
	rows := [][]string{{"Group", "Key", "Name", "Recipients", "Sent", "Clicked", "Submitted", "Reported", "Partial", "Suppressed"}}
	yes := func(v bool) string {
		if v {
			return "yes"
		}
		return ""
	}
	if results.Summary != nil {
		for _, l := range programSummaryLines(results.Summary, results.Anonymous) {
			rows = append(rows, []string{"Result", l.Key, l.Label, csvCount(l.Value), "", "", "", "", "", yes(l.Value == nil)})
		}
	}
	add := func(group string, r *ProgramResultRow) {
		rows = append(rows, []string{
			group,
			utils.CSVRemoveFormulaStart(r.Key),
			utils.CSVRemoveFormulaStart(r.Name),
			"",
			csvCount(r.Sent),
			csvCount(r.Clicked),
			csvCount(r.Submitted),
			csvCount(r.Reported),
			yes(r.Partial),
			yes(r.Suppressed),
		})
	}
	for _, m := range results.Months {
		add("Month", m)
	}
	for _, t := range results.Templates {
		add("Template", t)
	}
	return rows
}

// programExportBatch is how many sends the CSV export reads at a time
const programExportBatch = 1000

// programSendCSVHeader is the header of the per send export
var programSendCSVHeader = []string{
	"Email", "Name", "Template", "Kind", "Sent at", "Clicked link at",
	"Data submitted at", "Reported at", "Training started at", "Training completed at",
	"Language",
}

// programSendCSV writes one row per sent message of a normal program, oldest
// first. The rows come from the facts of the program, see programFactsFor,
// brought up to date first. The sends are copied in their small form under
// the read lock, so a slow reader of the export never holds up the facts,
// and the rows are built and written a batch at a time with the recipient
// names of the batch. No read of the database stays open while the rows are
// written.
func (s *Program) programSendCSV(ctx context.Context, program *model.Program, write func([]string) error) error {
	facts, err := s.programFactsFor(ctx, program, time.Now())
	if err != nil {
		return err
	}
	facts.mu.RLock()
	sends := make([]programFactSend, len(facts.sends))
	copy(sends, facts.sends)
	// the person of a send of a normal program is the recipient id, or the
	// pseudonym when the recipient is gone, which names no recipient
	people := make([]uuid.UUID, len(facts.persons))
	for id, i := range facts.persons {
		people[i] = id
	}
	templateNames := make([]string, len(facts.names))
	copy(templateNames, facts.names)
	templateKeys := make([]string, len(facts.templates))
	copy(templateKeys, facts.templates)
	stamps := make([]string, len(facts.languages))
	copy(stamps, facts.languages)
	facts.mu.RUnlock()
	templateLanguages, err := s.factTemplateLanguages(ctx, templateKeys)
	if err != nil {
		return err
	}
	// the language of a send is its stamp, or its template language when it
	// has none
	languageOf := func(sd *programFactSend) string {
		if code := stamps[sd.language]; code != "" {
			return code
		}
		return templateLanguages[templateKeys[sd.template]]
	}
	// oldest first, sends of the same time in the order they were read
	order := make([]int32, len(sends))
	for i := range order {
		order[i] = int32(i)
	}
	sort.SliceStable(order, func(i, j int) bool { return sends[order[i]].sentAt < sends[order[j]].sentAt })
	if err := write(programSendCSVHeader); err != nil {
		return errs.Wrap(err)
	}
	type who struct{ email, name string }
	known := map[uuid.UUID]who{}
	at := func(n int64) string {
		if n == programFactNone {
			return ""
		}
		return time.Unix(0, n).UTC().Format(time.RFC3339)
	}
	// once Anonymize All began the sends name no one, as the rows of an
	// anonymized campaign and the recipients view, also in the campaigns the
	// run has not reached yet
	anonymized := program.AnonymizeBegun()
	for start := 0; start < len(order); start += programExportBatch {
		batch := order[start:min(start+programExportBatch, len(order))]
		missing := []*uuid.UUID{}
		for _, i := range batch {
			id := people[sends[i].person]
			if _, ok := known[id]; !ok {
				known[id] = who{}
				missing = append(missing, &id)
			}
		}
		if anonymized {
			missing = nil
		}
		recipients, err := s.ProgramRepository.GetRecipientNames(ctx, missing)
		if err != nil {
			return errs.Wrap(err)
		}
		for _, r := range recipients {
			known[r.ID] = who{email: r.Email.String, name: fullName(r.FirstName.String, r.LastName.String)}
		}
		for _, i := range batch {
			sd := &sends[i]
			w := known[people[sd.person]]
			if anonymized {
				w = who{email: "anonymized", name: "anonymized"}
			}
			kind := ProgramSendKindPhishing
			row := []string{
				utils.CSVRemoveFormulaStart(w.email),
				utils.CSVRemoveFormulaStart(w.name),
				utils.CSVRemoveFormulaStart(templateNames[sd.template]),
				"",
				at(sd.sentAt),
				"", "", "", "", "",
				"",
			}
			// once Anonymize All began a send names no language either
			if !anonymized {
				row[10] = utils.CSVRemoveFormulaStart(languageOf(sd))
			}
			if sd.training {
				kind = ProgramSendKindTraining
				row[8] = at(sd.trainingStartedAt())
				row[9] = at(sd.out[factTrainingCompleted])
			} else {
				row[5] = at(sd.clickedAt())
				row[6] = at(sd.out[factSubmitted])
				row[7] = at(sd.out[factReported])
			}
			row[3] = kind
			if err := write(row); err != nil {
				return errs.Wrap(err)
			}
		}
	}
	return nil
}
