package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// auditCoverageSkip are functions that create an audit event and write a
// success, but do not need the deferred auditDone. Reads and timer jobs are
// skipped by name below.
var auditCoverageSkip = map[string]string{
	// the event is created after all work is done, nothing can fail after it
	"Session.Create": "event written last",
	// timer job, failures repeat on every run and go to the error log
	"Campaign.SchedulePendingCampaigns": "timer job",
}

var auditCoverageSkipName = regexp.MustCompile(
	`^(Get|List|Is|Preview|Search|Browse|Build|Count|Has|Find|Validate|Mask|Handle|Cleanup|PruneExpired)`,
)

// every service method that writes an audit success for a change must also
// defer auditDone, so a failure after the permission check is not lost
func TestAuditDoneCoverage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, missing := range missingAuditDone(t, path, src) {
			t.Errorf("%s: %s writes an audit success but does not defer auditDone", path, missing)
		}
	}
}

// the check itself finds a method without the defer
func TestAuditDoneCoverageFindsMissing(t *testing.T) {
	src := []byte(`package service
func (w *Webhook) Remove(session *model.Session) error {
	ae := NewAuditEvent("Webhook.Remove", session)
	w.AuditLogAuthorized(ae)
	return nil
}
func (w *Webhook) Keep(session *model.Session) (err error) {
	ae := NewAuditEvent("Webhook.Keep", session)
	defer w.auditDone(ae, &err)
	w.AuditLogAuthorized(ae)
	return nil
}
func (w *Webhook) GetOne(session *model.Session) error {
	ae := NewAuditEvent("Webhook.GetOne", session)
	w.AuditLogAuthorized(ae)
	return nil
}
`)
	got := missingAuditDone(t, "x.go", src)
	if len(got) != 1 || got[0] != "Webhook.Remove" {
		t.Fatalf("expected only Webhook.Remove, got %v", got)
	}
}

// every non read method that creates an audit event at the top must write it
// on some path, an event that is only filled is lost
func TestAuditEventsAreWritten(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Recv == nil || !hasTopLevelAuditEvent(fn) {
				continue
			}
			name := fn.Name.Name
			if auditCoverageSkipName.MatchString(strings.ToUpper(name[:1]) + name[1:]) {
				continue
			}
			body := string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
			written := false
			for _, call := range []string{"AuditLogAuthorized", "AuditLogNotAuthorized", "AuditLogFailed", "authorizeAndAudit", "auditImportDone"} {
				if strings.Contains(body, call) {
					written = true
				}
			}
			if !written {
				t.Errorf("%s: %s.%s creates an audit event but never writes it", path, receiverType(fn), name)
			}
		}
	}
}

// missingAuditDone lists methods in src that write an audit success for a
// change without deferring auditDone
func missingAuditDone(t *testing.T, path string, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		name := fn.Name.Name
		if auditCoverageSkipName.MatchString(strings.ToUpper(name[:1]) + name[1:]) {
			continue
		}
		res := fn.Type.Results
		if res == nil || len(res.List) == 0 {
			continue
		}
		if id, ok := res.List[len(res.List)-1].Type.(*ast.Ident); !ok || id.Name != "error" {
			continue
		}
		body := string(src[fset.Position(fn.Body.Pos()).Offset:fset.Position(fn.Body.End()).Offset])
		if !strings.Contains(body, "AuditLogAuthorized") || !hasTopLevelAuditEvent(fn) {
			continue
		}
		full := receiverType(fn) + "." + name
		if _, skip := auditCoverageSkip[full]; skip {
			continue
		}
		if !strings.Contains(body, "auditDone(ae") && !strings.Contains(body, "auditFailure(ae") {
			missing = append(missing, full)
		}
	}
	return missing
}

func hasTopLevelAuditEvent(fn *ast.FuncDecl) bool {
	for _, st := range fn.Body.List {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			continue
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "NewAuditEvent" {
				return true
			}
		}
	}
	return false
}

func receiverType(fn *ast.FuncDecl) string {
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
