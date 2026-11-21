package controller

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/api"
	"go.uber.org/zap"
)

// exportZipEntries is the content of the zips the export tests write
var exportZipEntries = []struct{ name, body string }{
	{"company.csv", "Created at,Name\n2026-01-01,Acme\n"},
	{"campaign_events/a.csv", strings.Repeat("event,row,with,some,text\n", 20000)},
	{"programs.csv", "Program,Anonymous,Events file\n"},
}

// newExportTest gives a company controller and a request context, with the
// temporary directory of the system pointed at a directory of the test
func newExportTest(t *testing.T) (*Company, *gin.Context, *httptest.ResponseRecorder, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	c := &Company{Common: Common{Response: api.NewJSONResponseHandler(), Logger: zap.NewNop().Sugar()}}
	w := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(w)
	g.Request = httptest.NewRequest(http.MethodGet, "/export", nil)
	return c, g, w, dir
}

// assertNoTempFiles fails when the export left a file behind
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read temp dir: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

// TestExportZipServesFile checks the zip written to a temporary file is sent
// byte for byte as the same zip built in memory, with its length, and that
// the file is deleted afterwards
func TestExportZipServesFile(t *testing.T) {
	c, g, w, dir := newExportTest(t)

	want := &bytes.Buffer{}
	ref := zip.NewWriter(want)
	for _, e := range exportZipEntries {
		f, err := ref.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ref.Close(); err != nil {
		t.Fatal(err)
	}

	func() {
		z, ok := c.newExportZip(g)
		if !ok {
			t.Fatal("could not create the export file")
		}
		defer z.remove()
		// the name is removed at once, the open file is still written and sent
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Fatalf("expected the export name removed at once, found %v", left)
		}
		for _, e := range exportZipEntries {
			f, err := z.Create(e.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
		c.responseWithExportZip(g, z, "export.zip")
	}()

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), want.Bytes()) {
		t.Fatalf("the sent zip differs from the zip built in memory (%d and %d bytes)", w.Body.Len(), want.Len())
	}
	if got := w.Header().Get("Content-Length"); got != fmt.Sprint(want.Len()) {
		t.Fatalf("Content-Length %q, want %d", got, want.Len())
	}
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type %q", got)
	}
	assertNoTempFiles(t, dir)
}

// TestExportZipErrorPartWay checks an error part way answers with an error
// and no zip, and the file is deleted
func TestExportZipErrorPartWay(t *testing.T) {
	c, g, w, dir := newExportTest(t)
	func() {
		z, ok := c.newExportZip(g)
		if !ok {
			t.Fatal("could not create the export file")
		}
		defer z.remove()
		f, err := z.Create(exportZipEntries[1].name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(exportZipEntries[1].body)); err != nil {
			t.Fatal(err)
		}
		// as a handler does when a read fails
		if ok := c.handleErrors(g, errors.New("read failed")); ok {
			t.Fatal("expected the error to be handled")
		}
	}()
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", w.Code)
	}
	if w.Header().Get("Content-Type") == "application/zip" || bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
		t.Fatal("a partial zip was sent")
	}
	assertNoTempFiles(t, dir)
}
