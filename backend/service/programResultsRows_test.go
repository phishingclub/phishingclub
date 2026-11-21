package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/model"
)

// trainingSend builds a training send of template for a person, sent daysAgo,
// started and completed as asked
func trainingSend(person uuid.UUID, daysAgo int, template string, started, completed bool) *programSend {
	s := &programSend{Person: person, Campaign: uuid.New(), SentAt: at(daysAgo), Template: template, Training: true}
	after := s.SentAt.Add(time.Hour)
	if started {
		s.TrainingStartedAt = tp(after)
	}
	if completed {
		s.TrainingCompletedAt = tp(after.Add(time.Hour))
	}
	return s
}

// rowByKey finds a row by its key
func rowByKey(t *testing.T, rows []*ProgramResultRow, key string) *ProgramResultRow {
	t.Helper()
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no row %q", key)
	return nil
}

func wantCount(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("%s = %v, want %d", name, got, want)
	}
}

func wantNull(t *testing.T, name string, got *int) {
	t.Helper()
	if got != nil {
		t.Fatalf("%s = %d, want null", name, *got)
	}
}

// TestProgramRowsMixed checks a mixed program counts the training sends in
// the month and template rows next to the phishing counts, which stay
// phishing only: a training template row holds no phishing counts, a phishing
// template row no training counts, and a completed training counts as started
func TestProgramRowsMixed(t *testing.T) {
	facts := &programFacts{templateNames: map[string]string{"t1": "Phish", "tr": "Course"}}
	ps := people(3)
	facts.sends = append(facts.sends,
		send(ps[0], 3, "clicked"), send(ps[1], 3, "reported"), send(ps[2], 3, "ignored"),
		trainingSend(ps[0], 3, "tr", true, true),
		trainingSend(ps[1], 3, "tr", false, true),
		trainingSend(ps[2], 3, "tr", true, false),
		// a month with only training
		trainingSend(ps[0], 60, "tr", false, false),
	)
	r := results(facts, time.UTC, resultsNow, nil, nil)
	if !r.Summary.HasPhishing || !r.Summary.HasTraining {
		t.Fatalf("a mixed program has phishing and training: %+v", r.Summary)
	}
	phish := rowByKey(t, r.Templates, "t1")
	wantCount(t, "phishing sent", phish.Sent, 3)
	wantCount(t, "phishing clicked", phish.Clicked, 1)
	wantCount(t, "phishing reported", phish.Reported, 1)
	wantNull(t, "phishing row training sent", phish.TrainingSent)
	wantNull(t, "phishing row training started", phish.TrainingStarted)
	course := rowByKey(t, r.Templates, "tr")
	wantNull(t, "training row sent", course.Sent)
	wantNull(t, "training row clicked", course.Clicked)
	wantCount(t, "training sent", course.TrainingSent, 4)
	wantCount(t, "training started", course.TrainingStarted, 3)
	wantCount(t, "training completed", course.TrainingCompleted, 2)
	// the most sent template comes first, phishing or training
	if r.Templates[0].Key != "tr" {
		t.Fatalf("the template with the most sends comes first, got %s", r.Templates[0].Key)
	}

	if len(r.Months) != 2 {
		t.Fatalf("want 2 months, got %d", len(r.Months))
	}
	last := r.Months[1]
	wantCount(t, "month sent", last.Sent, 3)
	wantCount(t, "month clicked", last.Clicked, 1)
	wantCount(t, "month training sent", last.TrainingSent, 3)
	wantCount(t, "month training started", last.TrainingStarted, 3)
	wantCount(t, "month training completed", last.TrainingCompleted, 2)
	// a month of only training shows 0 phishing sends, the program has phishing
	first := r.Months[0]
	wantCount(t, "training month sent", first.Sent, 0)
	wantCount(t, "training month clicked", first.Clicked, 0)
	wantCount(t, "training month training sent", first.TrainingSent, 1)
	wantCount(t, "training month training started", first.TrainingStarted, 0)
}

// TestProgramRowsPhishingOnly checks a program without training holds no
// training counts in its rows, as before training was counted per row
func TestProgramRowsPhishingOnly(t *testing.T) {
	facts := &programFacts{templateNames: map[string]string{"t1": "Phish"}}
	ps := people(2)
	facts.sends = append(facts.sends, send(ps[0], 3, "clicked"), send(ps[1], 3, "submitted"))
	r := results(facts, time.UTC, resultsNow, nil, nil)
	if !r.Summary.HasPhishing || r.Summary.HasTraining {
		t.Fatalf("a phishing program has phishing only: %+v", r.Summary)
	}
	for _, row := range append(append([]*ProgramResultRow{}, r.Months...), r.Templates...) {
		wantCount(t, "sent", row.Sent, 2)
		wantCount(t, "clicked", row.Clicked, 2)
		wantCount(t, "submitted", row.Submitted, 1)
		wantNull(t, "training sent", row.TrainingSent)
		wantNull(t, "training started", row.TrainingStarted)
		wantNull(t, "training completed", row.TrainingCompleted)
	}
}

// TestProgramRowsTrainingOnly checks a program of only training holds no
// phishing counts in its rows and summary flags, and its report shows no
// phishing card, outcome chart, trend or phishing column
func TestProgramRowsTrainingOnly(t *testing.T) {
	facts := &programFacts{templateNames: map[string]string{"tr": "Course"}}
	ps := people(2)
	facts.sends = append(facts.sends,
		trainingSend(ps[0], 3, "tr", true, true),
		trainingSend(ps[1], 40, "tr", true, false),
	)
	r := results(facts, time.UTC, resultsNow, nil, nil)
	if r.Summary.HasPhishing || !r.Summary.HasTraining {
		t.Fatalf("a training program has training only: %+v", r.Summary)
	}
	if len(r.Months) != 2 || len(r.Templates) != 1 {
		t.Fatalf("want 2 months and 1 template, got %d and %d", len(r.Months), len(r.Templates))
	}
	for _, row := range append(append([]*ProgramResultRow{}, r.Months...), r.Templates...) {
		wantNull(t, "sent", row.Sent)
		wantNull(t, "clicked", row.Clicked)
		wantNull(t, "submitted", row.Submitted)
		wantNull(t, "reported", row.Reported)
		if row.TrainingSent == nil || row.TrainingStarted == nil || row.TrainingCompleted == nil {
			t.Fatalf("training counts must be set: %+v", row)
		}
	}
	wantCount(t, "template training sent", r.Templates[0].TrainingSent, 2)
	wantCount(t, "template training started", r.Templates[0].TrainingStarted, 2)
	wantCount(t, "template training completed", r.Templates[0].TrainingCompleted, 1)

	program := &model.Program{Status: model.ProgramStatusActive}
	d := programReportFromResults(program, &programReportSetup{}, r, resultsNow, nil, nil)
	for _, card := range d.Results {
		switch card.Label {
		case "Clicked link", "Data submitted", "Reported", "Repeat offenders":
			t.Fatalf("a training program report shows no %s card", card.Label)
		}
	}
	// Recipients, Sent from the training sends as on the program page, and the
	// 2 training cards
	if len(d.Results) != 4 || len(d.Outcome) != 0 || len(d.Trend) != 0 {
		t.Fatalf("want Recipients, Sent and the 2 training cards, no outcome and no trend: %d %d %d", len(d.Results), len(d.Outcome), len(d.Trend))
	}
	if d.Results[1].Label != "Sent" || d.Results[1].Value != "2" {
		t.Fatalf("want Sent from the training sends, got %s %s", d.Results[1].Label, d.Results[1].Value)
	}
	if d.Results[2].Sub != "100% of training sent" {
		t.Fatalf("want Training started as a share of training sent, got %q", d.Results[2].Sub)
	}
	// name, training sent, started, completed
	if cells := d.Templates[0].Cells; len(cells) != 4 || cells[1] != "2" || cells[2] != "2" || cells[3] != "1" {
		t.Fatalf("template cells %v", cells)
	}
	if pcts := d.Templates[0].Pcts; pcts[2] != "100%" || pcts[3] != "50%" {
		t.Fatalf("template percents %v", pcts)
	}
	html, err := renderProgramReportContent(defaultReportContentForKind(model.ReportKindProgram), d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, text := range []string{"Clicked link", "Repeat offenders", "Click Rate"} {
		if strings.Contains(html, text) {
			t.Fatalf("a training program report must not mention %q", text)
		}
	}
	if !strings.Contains(html, "Training completed") {
		t.Fatalf("a training program report shows the training columns")
	}
	// the export has no phishing lines either
	for _, l := range programSummaryLines(r.Summary, false) {
		switch l.Key {
		case "sent", "clickedLink", "submittedData", "reported", "repeatOffenders":
			t.Fatalf("a training program export has no %s line", l.Key)
		}
	}
}

// TestProgramRowsMixedReportCells checks a mixed program report row lists the
// phishing cells then the training cells, a count the row does not hold as
// "-"
func TestProgramRowsMixedReportCells(t *testing.T) {
	row := programReportResultRow("Course", &ProgramResultRow{
		TrainingSent: intPtr(4), TrainingStarted: intPtr(2), TrainingCompleted: intPtr(1),
	}, true, true)
	want := []string{"Course", "-", "-", "-", "-", "4", "2", "1"}
	if strings.Join(row.Cells, ",") != strings.Join(want, ",") {
		t.Fatalf("cells %v, want %v", row.Cells, want)
	}
	if row.Pcts[6] != "50%" || row.Pcts[7] != "50%" || row.Pcts[2] != "" {
		t.Fatalf("percents %v", row.Pcts)
	}
	// a phishing only report row keeps its 5 cells
	row = programReportResultRow("Phish", &ProgramResultRow{Sent: intPtr(2), Clicked: intPtr(1), Submitted: intPtr(0), Reported: intPtr(1)}, true, false)
	if strings.Join(row.Cells, ",") != "Phish,2,1,0,1" {
		t.Fatalf("phishing cells %v", row.Cells)
	}
}

// TestProgramRowsAnonymousHideTraining checks an anonymous program holds no
// training counts in its rows, as it holds no phishing outcomes there: the
// training outcomes are in the summary only, and its rows stay the phishing
// sizes they were
func TestProgramRowsAnonymousHideTraining(t *testing.T) {
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"t1": "Phish", "tr": "Course"}}
	for i, p := range people(12) {
		facts.sends = append(facts.sends, send(p, 10, "clicked"), trainingSend(p, 10, "tr", i < 8, i < 6))
	}
	r := results(facts, time.UTC, anonymousNow, nil, nil)
	if !r.Summary.HasPhishing || !r.Summary.HasTraining {
		t.Fatalf("the program has phishing and training: %+v", r.Summary)
	}
	for _, row := range append(append([]*ProgramResultRow{}, r.Months...), r.Templates...) {
		if row.Key == "tr" {
			t.Fatalf("an anonymous program shows no training template row")
		}
		if !row.OutcomesHidden {
			t.Fatalf("an anonymous row hides its outcomes: %+v", row)
		}
		wantNull(t, "clicked", row.Clicked)
		wantNull(t, "training sent", row.TrainingSent)
		wantNull(t, "training started", row.TrainingStarted)
		wantNull(t, "training completed", row.TrainingCompleted)
		wantCount(t, "sent", row.Sent, 12)
	}
	if len(r.Months) != 1 || len(r.Templates) != 1 {
		t.Fatalf("want the 1 phishing month and template, got %d and %d", len(r.Months), len(r.Templates))
	}
	// the report shows the training columns hidden, never a count
	row := programReportResultRow("Jun 2025", r.Months[0], true, true)
	for _, c := range row.Cells[2:] {
		if c != programWithheldText {
			t.Fatalf("an anonymous row cell must be hidden, got %v", row.Cells)
		}
	}
}

// TestProgramTilesHasPhishing checks the tiles carry whether the program has
// phishing from the steps or the sends
func TestProgramTilesHasPhishing(t *testing.T) {
	facts := &programFacts{templateNames: map[string]string{"tr": "Course"}}
	facts.sends = append(facts.sends, trainingSend(uuid.New(), 3, "tr", true, false))
	in := programResultsInput{loc: time.UTC, now: resultsNow, repeatMonths: 12}
	period := programTilesPeriod{key: "all"}
	all := buildProgramResults(facts, in).Summary
	tiles := buildProgramTiles(facts, programResultsInput{loc: time.UTC, now: resultsNow, hasTraining: all.HasTraining, hasPhishing: all.HasPhishing}, period, false, all)
	if tiles.HasPhishing || !tiles.HasTraining {
		t.Fatalf("a training program has no phishing tiles: %+v", tiles)
	}
	wantCount(t, "training sent", tiles.TrainingSent, 1)
	// a phishing step not sent yet still makes the program a phishing program
	in.hasPhishing = true
	if !buildProgramResults(facts, in).Summary.HasPhishing {
		t.Fatalf("a phishing step sets HasPhishing")
	}
}

// TestProgramAnonymousTrainingOnlyReport checks the report and the CSV export
// of an anonymous program that only sends training: the cards are Recipients,
// Sent from the training sends and the 2 training cards, there are no month
// or template rows, the PDF says the training results are in the totals only,
// and the CSV totals hold a Training sent line. A small program withholds Sent.
func TestProgramAnonymousTrainingOnlyReport(t *testing.T) {
	program := &model.Program{Status: model.ProgramStatusActive, IsAnonymous: nullable.NewNullableWithValue(true)}
	facts := &programFacts{anonymous: true, templateNames: map[string]string{"tr": "Course"}}
	for i, p := range people(20) {
		facts.sends = append(facts.sends, trainingSend(p, 10, "tr", i < 12, i < 6))
	}
	r := buildProgramResults(facts, programResultsInput{loc: time.UTC, now: anonymousNow, recipients: 20, repeatMonths: 12})
	if r.Summary.HasPhishing || !r.Summary.HasTraining || !r.Summary.HasSent {
		t.Fatalf("a sent training program: %+v", r.Summary)
	}
	if len(r.Months) != 0 || len(r.Templates) != 0 {
		t.Fatalf("an anonymous program keeps training out of its rows, got %d months and %d templates", len(r.Months), len(r.Templates))
	}
	d := programReportFromResults(program, &programReportSetup{}, r, anonymousNow, nil, nil)
	want := [][2]string{{"Recipients", "20"}, {"Sent", "20"}, {"Training started", "12"}, {"Training completed", "6"}}
	if len(d.Results) != len(want) {
		t.Fatalf("want %d cards, got %+v", len(want), d.Results)
	}
	for i, w := range want {
		l := d.Results[i]
		if l.Label != w[0] || l.Value != w[1] || l.Withheld {
			t.Fatalf("card %d = %s %s, want %s %s", i, l.Label, l.Value, w[0], w[1])
		}
		// an anonymous program counts sends, so a card shows no share
		if l.Sub != "" {
			t.Fatalf("an anonymous card has no share: %+v", l)
		}
	}
	if len(d.Outcome) != 0 || len(d.Trend) != 0 || len(d.Months) != 0 || len(d.Templates) != 0 {
		t.Fatalf("no outcome, trend or tables: %+v", d)
	}
	html, err := renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "Anonymous programs show training results in the totals only") {
		t.Fatalf("the report must say why there are no tables")
	}
	for _, gone := range []string{"Trend by Month", "Results by Template", "Nothing was sent in this period.", "Clicked link"} {
		if strings.Contains(html, gone) {
			t.Fatalf("the report must leave out %q", gone)
		}
	}
	// the CSV holds the totals with Training sent and no rows
	csv := programAggregateCSV(r)
	keys := []string{}
	for _, row := range csv[1:] {
		if row[0] != "Result" {
			t.Fatalf("an anonymous training program has no %s rows: %v", row[0], row)
		}
		keys = append(keys, row[1]+"="+row[3])
	}
	if got := strings.Join(keys, ","); got != "recipients=20,trainingSent=20,trainingStarted=12,trainingCompleted=6" {
		t.Fatalf("csv totals %s", got)
	}

	// 4 people could be singled out, so every send count is withheld
	facts.sends = facts.sends[:4]
	r = buildProgramResults(facts, programResultsInput{loc: time.UTC, now: anonymousNow, recipients: 4, repeatMonths: 12})
	d = programReportFromResults(program, &programReportSetup{}, r, anonymousNow, nil, nil)
	if l := d.Results[1]; l.Label != "Sent" || !l.Withheld || l.Value != programWithheldText {
		t.Fatalf("a small training program withholds Sent: %+v", l)
	}
	for _, row := range programAggregateCSV(r)[1:] {
		if row[1] == "trainingSent" && (row[3] != "" || row[9] != "yes") {
			t.Fatalf("a withheld training sent line has no value: %v", row)
		}
	}
}

// TestProgramReportTrainingOnlyHeader checks the month and template tables of
// a training only program name the training sends Sent, as the Sent card
// does, while a mixed program keeps Training sent
func TestProgramReportTrainingOnlyHeader(t *testing.T) {
	d := programReportSampleData()
	out, err := renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, ">Training sent</th>") {
		t.Fatalf("a mixed program table names the training sends Training sent")
	}
	d.HasPhishing = false
	out, err = renderProgramReportContent(embedded.DefaultProgramReportHTML, d)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out, ">Training sent</th>") || !strings.Contains(out, `style="width:100px">Sent</th>`) {
		t.Fatalf("a training only program table names the training sends Sent")
	}
}
