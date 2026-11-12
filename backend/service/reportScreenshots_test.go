package service

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"text/template"
	"time"

	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/model"
	"go.uber.org/zap"
)

func TestReportScreenshotsWanted(t *testing.T) {
	all := reportScreenshotWants{email: true, before: true, landing: true, after: true}
	cases := []struct {
		content  string
		single   reportScreenshotWants
		list     reportScreenshotWants
		listUsed bool
	}{
		{`{{.CampaignName}}`, reportScreenshotWants{}, reportScreenshotWants{}, false},
		{`{{if .BeforeLandingPageScreenshot}}x{{end}}`, reportScreenshotWants{before: true}, reportScreenshotWants{}, false},
		{`{{.LandingPageScreenshot}}`, reportScreenshotWants{landing: true}, reportScreenshotWants{}, false},
		{`{{.AfterLandingPageScreenshot}}{{.EmailScreenshot}}`, reportScreenshotWants{email: true, after: true}, reportScreenshotWants{}, false},
		{`{{range .TemplateScreenshots}}{{.Email}}{{end}}`, reportScreenshotWants{}, reportScreenshotWants{email: true}, true},
		{`{{range $t := .TemplateScreenshots}}{{$t.BeforeLandingPage}}{{end}}{{.EmailScreenshot}}`, reportScreenshotWants{email: true}, reportScreenshotWants{before: true}, true},
		{`{{range .TemplateScreenshots}}{{.Name}}{{end}}`, reportScreenshotWants{}, reportScreenshotWants{}, true},
		{`{{range .TemplateScreenshots}}{{.LandingPage}}{{.AfterLandingPage}}{{.Email}}{{.BeforeLandingPage}}{{end}}`, reportScreenshotWants{}, all, true},
	}
	for _, tc := range cases {
		single, list, listUsed := reportScreenshotsWanted(tc.content)
		if single != tc.single || list != tc.list || listUsed != tc.listUsed {
			t.Errorf("%q: got %+v %+v %v, want %+v %+v %v", tc.content, single, list, listUsed, tc.single, tc.list, tc.listUsed)
		}
	}
}

func TestReportScreenshotsFor(t *testing.T) {
	email := reportScreenshotWants{email: true}
	landing := reportScreenshotWants{landing: true}

	// single fields only: the first template, nothing after it
	if w, ok := reportScreenshotsFor(0, email, reportScreenshotWants{}, false); !ok || w != email {
		t.Fatalf("first template with single fields: got %+v %v", w, ok)
	}
	if _, ok := reportScreenshotsFor(1, email, reportScreenshotWants{}, false); ok {
		t.Fatal("later templates must not be captured without the list")
	}
	// the list: the first template serves both, later ones only the list
	if w, ok := reportScreenshotsFor(0, email, landing, true); !ok || w != (reportScreenshotWants{email: true, landing: true}) {
		t.Fatalf("first template with list: got %+v %v", w, ok)
	}
	if w, ok := reportScreenshotsFor(3, email, landing, true); !ok || w != landing {
		t.Fatalf("later template with list: got %+v %v", w, ok)
	}
	// a list that only shows names still gets an entry per template
	if w, ok := reportScreenshotsFor(2, reportScreenshotWants{}, reportScreenshotWants{}, true); !ok || w.any() {
		t.Fatalf("names only list: got %+v %v", w, ok)
	}
	// the cap
	if _, ok := reportScreenshotsFor(reportScreenshotMaxTemplates-1, email, landing, true); !ok {
		t.Fatal("the last template under the cap must be captured")
	}
	if _, ok := reportScreenshotsFor(reportScreenshotMaxTemplates, email, landing, true); ok {
		t.Fatal("templates past the cap must not be captured")
	}
}

func renderReport(t *testing.T, content string, rd *model.ReportData) string {
	t.Helper()
	tmpl, err := template.New("report").Funcs(TemplateFuncs()).Parse(content)
	if err != nil {
		t.Fatalf("parse default report: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, htmlEscapeReportData(rd)); err != nil {
		t.Fatalf("render default report: %v", err)
	}
	return buf.String()
}

func TestDefaultReportScreenshotSections(t *testing.T) {
	for name, content := range map[string]string{
		"simulation": embedded.DefaultReportHTML,
		"training":   embedded.DefaultTrainingReportHTML,
	} {
		t.Run(name, func(t *testing.T) {
			testReportScreenshotSections(t, content)
		})
	}
}

func testReportScreenshotSections(t *testing.T, content string) {
	all := reportScreenshotWants{email: true, before: true, landing: true, after: true}
	single, list, listUsed := reportScreenshotsWanted(content)
	if single != all || list != all || !listUsed {
		t.Fatalf("default report must use every screenshot, got %+v %+v %v", single, list, listUsed)
	}

	// one template: the single pages, no template caption
	one := &model.ReportData{
		EmailScreenshot:       "data:image/jpeg;base64,EMAIL1",
		LandingPageScreenshot: "data:image/jpeg;base64,LANDING1",
		TemplateScreenshots: []model.ReportTemplateScreenshots{
			{Number: 1, Name: "First", Email: "data:image/jpeg;base64,EMAIL1", LandingPage: "data:image/jpeg;base64,LANDING1"},
		},
		TemplateCount: 1,
	}
	out := renderReport(t, content, one)
	if strings.Count(out, `src="data:image/jpeg;base64,EMAIL1"`) != 1 || strings.Count(out, `src="data:image/jpeg;base64,LANDING1"`) != 1 {
		t.Fatal("one template report must show each screenshot once")
	}
	if strings.Contains(out, "Template 1 of") {
		t.Fatal("one template report must not show the per template section")
	}

	// several templates: every template, captioned, the single pages not repeated
	several := &model.ReportData{
		EmailScreenshot: "data:image/jpeg;base64,EMAIL1",
		TemplateScreenshots: []model.ReportTemplateScreenshots{
			{Number: 1, Name: "First", Email: "data:image/jpeg;base64,EMAIL1"},
			{Number: 2, Name: `<b>Second</b>`, Email: "data:image/jpeg;base64,EMAIL2", AfterLandingPage: "data:image/jpeg;base64,AFTER2"},
		},
		TemplateCount: 12,
	}
	out = renderReport(t, content, several)
	for _, want := range []string{
		`src="data:image/jpeg;base64,EMAIL1"`,
		`src="data:image/jpeg;base64,EMAIL2"`,
		`src="data:image/jpeg;base64,AFTER2"`,
		"Template 1 of 12: First",
		"Template 2 of 12: &lt;b&gt;Second&lt;/b&gt;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("several template report is missing %q", want)
		}
	}
	if strings.Count(out, `src="data:image/jpeg;base64,EMAIL1"`) != 1 {
		t.Error("several template report must not repeat the single pages")
	}
	if strings.Contains(out, "<b>Second</b>") {
		t.Error("template name was not escaped")
	}
	if !strings.Contains(out, "Screenshots are taken of the first 2 templates.") {
		t.Error("a pool larger than the list must say so")
	}

	// the first template failed to load: the section still shows, and says
	// which template has no screenshots
	failedFirst := &model.ReportData{
		TemplateScreenshots: []model.ReportTemplateScreenshots{
			{Number: 1},
			{Number: 2, Name: "Second", Email: "data:image/jpeg;base64,EMAIL2"},
		},
		TemplateCount: 2,
	}
	out = renderReport(t, content, failedFirst)
	if !strings.Contains(out, `src="data:image/jpeg;base64,EMAIL2"`) {
		t.Error("the second template must show when the first failed")
	}
	if !strings.Contains(out, "No screenshots are available for this template.") {
		t.Error("a template without screenshots must say so")
	}
	if strings.Contains(out, "Screenshots are taken of the first") {
		t.Error("a pool that fits the list must not mention a limit")
	}
}

func TestRunReportCapturesBoundsParallelAndKeepsOrder(t *testing.T) {
	var jobs []*reportCaptureJob
	for i := 0; i < 10; i++ {
		jobs = append(jobs, &reportCaptureJob{what: fmt.Sprint(i), html: fmt.Sprint(i)})
	}
	var running, most atomic.Int64
	runReportCaptures(jobs, 3, 1<<20, zap.NewNop().Sugar(), func(job *reportCaptureJob) ([]byte, error) {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		// later jobs finish first
		i, _ := strconv.Atoi(job.html)
		time.Sleep(time.Duration(10-i) * 3 * time.Millisecond)
		running.Add(-1)
		if i == 4 {
			return nil, errors.New("failed")
		}
		return []byte(job.html), nil
	})
	if most.Load() > 3 {
		t.Fatalf("%d captures ran at once, want at most 3", most.Load())
	}
	for i, job := range jobs {
		want := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte(fmt.Sprint(i)))
		if i == 4 {
			want = ""
		}
		if job.uri != want {
			t.Fatalf("job %d has uri %q, want %q", i, job.uri, want)
		}
	}
}

func TestRunReportCapturesSizeLimit(t *testing.T) {
	uriLen := len("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(make([]byte, 30)))
	var jobs []*reportCaptureJob
	for i := 0; i < 6; i++ {
		jobs = append(jobs, &reportCaptureJob{what: fmt.Sprint(i)})
	}
	var calls atomic.Int64
	// room for two screenshots, run one at a time so the skip is exact
	runReportCaptures(jobs, 1, 2*uriLen+1, zap.NewNop().Sugar(), func(job *reportCaptureJob) ([]byte, error) {
		calls.Add(1)
		return make([]byte, 30), nil
	})
	// the third capture runs as the taken bytes are still below the limit,
	// then left out in order, and no capture starts after it
	if calls.Load() != 3 {
		t.Fatalf("%d captures ran, want 3", calls.Load())
	}
	for i, job := range jobs {
		if (job.uri != "") != (i < 2) {
			t.Fatalf("job %d kept %v", i, job.uri != "")
		}
	}
}
