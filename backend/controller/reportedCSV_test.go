package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/service"
)

// reportedCSVUpload runs readReportedCSVUpload on a file of the given rows
func reportedCSVUpload(t *testing.T, dataRows int) (int, bool, int) {
	t.Helper()
	var csvBody strings.Builder
	csvBody.WriteString("email\n")
	for i := 0; i < dataRows; i++ {
		csvBody.WriteString("a@example.com\n")
	}
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", "reported.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(csvBody.String())); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	g, _ := gin.CreateTestContext(rec)
	g.Request = httptest.NewRequest(http.MethodPost, "/", body)
	g.Request.Header.Set("Content-Type", mw.FormDataContentType())
	_, rows, ok := readReportedCSVUpload(g, api.NewJSONResponseHandler())
	return len(rows), ok, rec.Code
}

func TestReadReportedCSVUploadRowCap(t *testing.T) {
	n, ok, _ := reportedCSVUpload(t, service.ReportedCSVMaxRows)
	if !ok || n != service.ReportedCSVMaxRows {
		t.Fatalf("a file at the row cap must be read, got ok=%v rows=%d", ok, n)
	}
	n, ok, code := reportedCSVUpload(t, service.ReportedCSVMaxRows+1)
	if ok || n != 0 {
		t.Fatalf("a file over the row cap must be refused, got ok=%v rows=%d", ok, n)
	}
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a file over the row cap, got %d", code)
	}
}
