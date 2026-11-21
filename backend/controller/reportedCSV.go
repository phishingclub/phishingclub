package controller

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/service"
)

// reportedCSVMaxBytes caps the size of an uploaded reported CSV
const reportedCSVMaxBytes = 32 << 20

// readReportedCSVUpload reads the "file" field of a reported CSV upload row by
// row, keeping the line each row starts on. Rows may have fewer columns than
// the header, the import reports those per row. A broken file, such as an
// unclosed quote, rejects the whole upload since the rows after it can not be
// trusted. ok is false when a response was written.
func readReportedCSVUpload(g *gin.Context, response api.JSONResponseHandler) (header []string, rows []service.ReportedCSVRow, ok bool) {
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, reportedCSVMaxBytes)
	file, fileHeader, err := g.Request.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			response.ValidationFailed(g, "file", errors.New("file is larger than 32 MB"))
			return nil, nil, false
		}
		response.ValidationFailed(g, "file", err)
		return nil, nil, false
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(fileHeader.Filename), ".csv") {
		response.ValidationFailed(g, "file", errors.New("file must be a CSV"))
		return nil, nil, false
	}
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows = []service.ReportedCSVRow{}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			response.ValidationFailed(g, "file", errors.New("failed to parse CSV file: "+err.Error()))
			return nil, nil, false
		}
		if header == nil {
			header = record
			continue
		}
		// a file with too many rows is refused without reading the rest
		if len(rows) >= service.ReportedCSVMaxRows {
			response.ValidationFailed(g, "file", fmt.Errorf("CSV has more than %d rows", service.ReportedCSVMaxRows))
			return nil, nil, false
		}
		line, _ := reader.FieldPos(0)
		rows = append(rows, service.ReportedCSVRow{Line: line, Fields: record})
	}
	if len(rows) == 0 {
		response.ValidationFailed(g, "file", errors.New("CSV file must have header and at least one data row"))
		return nil, nil, false
	}
	return header, rows, true
}
