package controller

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/service"
)

// GetResults returns the program results by month and by template, with the
// result tiles for the time range in the range query value, one of the time
// range keys, or the default of the program when it is not given.
func (c *Program) GetResults(g *gin.Context) {
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
	timeRange, ok := programTimeRangeQuery(g, c.Response)
	if !ok {
		return
	}
	results, err := c.ProgramService.GetResults(g.Request.Context(), session, id, timeRange)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, results)
}

// GetResultGroups returns the results of a program grouped by the current
// department or position of each recipient or by language, named by the by
// query value
func (c *Program) GetResultGroups(g *gin.Context) {
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
	by := g.Query("by")
	if !service.IsValidProgramGroupBy(by) {
		c.Response.ValidationFailed(g, "by", errors.New("must be department, position or language"))
		return
	}
	groups, err := c.ProgramService.GetResultGroups(g.Request.Context(), session, id, by)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, groups)
}

// programTimeRangeQuery reads the optional range query value. A value that is
// not a time range key is refused with 400.
func programTimeRangeQuery(g *gin.Context, response api.JSONResponseHandler) (string, bool) {
	values, given := g.GetQueryArray("range")
	if !given {
		return "", true
	}
	if len(values) != 1 || !service.IsValidProgramTimeRange(values[0]) {
		response.ValidationFailed(g, "range", errors.New("must be 30d, 90d, 12m or all"))
		return "", false
	}
	return values[0], true
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
	export, err := c.ProgramService.ExportCSV(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// the rows are written as they are read, so a large program is never held
	// as a whole
	setSecureContentDisposition(g, "program-"+sanitizeProgramFilename(export.Name)+".csv")
	g.Header("Content-Type", "text/csv")
	err = writeCSVStream(g, func(write func([]string) error) error {
		err := export.Write(g.Request.Context(), write)
		if err != nil {
			c.Logger.Errorw("failed to write the program export", "error", err)
		}
		return err
	})
	if err != nil {
		c.handleErrors(g, err)
	}
}

// writeCSVStream writes the rows of run to the response as they come. The
// csv writer holds the first rows in its buffer, so an error before any byte
// reached the client is returned with the headers of the file cleared, for the
// caller to answer with an error. An error after bytes were sent can not be
// answered, so the response is cut with http.ErrAbortHandler, which the server
// ends without the last chunk, and the client sees a broken download and not a
// short file that looks complete.
func writeCSVStream(g *gin.Context, run func(write func([]string) error) error) error {
	writer := csv.NewWriter(g.Writer)
	err := run(writer.Write)
	if err == nil {
		writer.Flush()
		err = writer.Error()
	}
	if err == nil {
		return nil
	}
	if !g.Writer.Written() {
		g.Header("Content-Disposition", "")
		g.Header("Content-Type", "")
		return err
	}
	_ = g.Error(err)
	panic(http.ErrAbortHandler)
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
	pdf, name, err := c.ProgramService.ExportReportPDF(g.Request.Context(), session, id, g.Query("from"), g.Query("to"))
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.responseWithPDF(g, pdf, "program-report-"+sanitizeProgramFilename(name)+".pdf")
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
	header, rows, ok := readReportedCSVUpload(g, c.Response)
	if !ok {
		return
	}
	result, err := c.ProgramService.ProcessReportedCSV(g.Request.Context(), session, id, header, rows, emailColumn, dateColumn)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}
