package controller

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// programReportedMaxBytes bounds the size of a reported CSV upload
const programReportedMaxBytes = 10 << 20

// GetRecipientPrograms returns the programs a recipient is enrolled in.
func (c *Program) GetRecipientPrograms(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	programs, err := c.ProgramService.GetRecipientPrograms(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, programs)
}

// ExportCSV returns the program results as a CSV file.
func (c *Program) ExportCSV(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	rows, name, err := c.ProgramService.ExportCSV(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)
	if err := writer.WriteAll(rows); err != nil {
		c.handleErrors(g, err)
		return
	}
	c.responseWithCSV(g, buffer, writer, "program-"+sanitizeFilename(name)+".csv")
}

// ReportPDF returns the program report as a PDF for an optional period. A
// named program reads from and to as whole days. An anonymous program needs
// whole calendar months, from on the first day of a month and to on the first
// day of a later month, not included, or neither for all time.
func (c *Program) ReportPDF(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// from and to are YYYY-MM-DD dates or RFC 3339 times, read in the program
	// time zone by the service
	pdf, name, err := c.ProgramService.BuildReportPDF(g.Request.Context(), session, id, g.Query("from"), g.Query("to"))
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.responseWithPDF(g, pdf, "program-report-"+sanitizeFilename(name)+".pdf")
}

// UploadReportedCSV records the reports of a reported CSV on a program. The
// emailColumn query value is required, dateColumn is optional.
func (c *Program) UploadReportedCSV(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	emailColumn, err := strconv.Atoi(g.Query("emailColumn"))
	if err != nil || emailColumn < 0 {
		c.Response.ValidationFailed(g, "emailColumn", errors.New("emailColumn must be a valid non negative integer"))
		return
	}
	dateColumn := -1
	if v := g.Query("dateColumn"); v != "" {
		dateColumn, err = strconv.Atoi(v)
		if err != nil || dateColumn < 0 {
			c.Response.ValidationFailed(g, "dateColumn", errors.New("dateColumn must be a valid non negative integer"))
			return
		}
	}
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, programReportedMaxBytes+(1<<20))
	file, header, err := g.Request.FormFile("file")
	if err != nil {
		c.Response.ValidationFailed(g, "file", errors.New("a CSV file of at most 10 MB is required"))
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		c.Response.ValidationFailed(g, "file", errors.New("file must be a CSV"))
		return
	}
	content, err := io.ReadAll(io.LimitReader(file, programReportedMaxBytes+1))
	if err != nil {
		c.Response.ValidationFailed(g, "file", err)
		return
	}
	if len(content) > programReportedMaxBytes {
		c.Response.ValidationFailed(g, "file", errors.New("file is larger than 10 MB"))
		return
	}
	reader := csv.NewReader(bytes.NewReader(content))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		c.Response.ValidationFailed(g, "file", errors.New("failed to parse CSV file"))
		return
	}
	result, err := c.ProgramService.ProcessReportedCSV(g.Request.Context(), session, id, records, emailColumn, dateColumn)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}
