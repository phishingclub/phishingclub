package controller

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
)

// DomainColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var CompanyColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.COMPANY_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.COMPANY_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.COMPANY_TABLE, "name"),
	"comment":    repository.TableColumn(database.COMPANY_TABLE, "comment"),
}

// Company is a Company controller
type Company struct {
	Common
	CompanyService   *service.Company
	CampaignService  *service.Campaign
	RecipientService *service.Recipient
}

// GetByID gets a company by id
func (c *Company) GetByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	companyID, err := uuid.Parse(g.Param("id"))
	if err != nil {
		// ignore err as caused by bad user input
		_ = err
		c.Response.BadRequestMessage(g, api.InvalidCompanyID)
		return
	}
	// get company
	ctx := g.Request.Context()
	company, err := c.CompanyService.GetByID(
		ctx,
		session,
		&companyID,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, company)
}

// ExportByCompanyID sends a zip of CSV files with the company, its
// recipients, the events of each of its campaigns and the events of each of
// its programs. Recipients are read one page at a time and events one row at
// a time, each written straight into its zip entry.
func (c *Company) ExportByCompanyID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	if !c.handleExportAuthorization(g, session, companyID) {
		return
	}
	// get the company exported
	company, err := c.CompanyService.GetByID(
		g,
		session,
		companyID,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// the export holds recipients and campaign events, so it is audited
	done, err := c.CompanyService.AuthorizeExport(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// the event is written once the file is built and sent, or as failed
	built := false
	defer func() { done(built) }()
	// the zip is written to a temporary file and sent once it is complete
	zipWriter, ok := c.newExportZip(g)
	if !ok {
		return
	}
	defer zipWriter.remove()
	zipFileName := fmt.Sprintf("company_export_%s.zip", company.Name.MustGet().String())

	// add company data to zip
	{
		buffer := &bytes.Buffer{}
		writer := csv.NewWriter(buffer)
		headers := []string{
			"Created at",
			"Updated at",
			"Name",
			"Comment",
		}
		err = writer.Write(headers)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		row := []string{
			utils.CSVFromDate(company.CreatedAt),
			utils.CSVFromDate(company.UpdatedAt),
			utils.CSVRemoveFormulaStart(utils.NullableToString(company.Name)),
			utils.CSVRemoveFormulaStart(utils.NullableToString(company.Comment)),
		}
		err = writer.Write(row)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		writer.Flush()
		// add to zip
		f, err := zipWriter.Create("company.csv")
		if ok := c.handleErrors(g, err); !ok {
			return
		}
		_, err = f.Write(buffer.Bytes())
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	if ok := c.addRecipientsToZip(g, session, companyID, zipWriter.Writer); !ok {
		return
	}
	if ok := c.addCampaignEventsToZip(g, session, companyID, zipWriter.Writer); !ok {
		return
	}
	// program sends, named by program and template
	if ok := c.addProgramEventsToZip(g, session, companyID, zipWriter.Writer); !ok {
		return
	}
	built = c.responseWithExportZip(g, zipWriter, zipFileName)
}

// addRecipientsToZip adds recipients.csv with the recipients of exactly the
// company, or the shared recipients when companyID is nil, and a column per
// group for as many groups as the recipient in the most groups has. The
// recipients are read one page at a time and written straight into the zip
// entry. It returns false when a response was written.
func (c *Company) addRecipientsToZip(
	g *gin.Context,
	session *model.Session,
	companyID *uuid.UUID,
	zipWriter *zip.Writer,
) bool {
	ctx := g.Request.Context()
	maxGroups, err := c.RecipientService.GetExportGroupCount(ctx, session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return false
	}
	headers := recipientExportHeaders(maxGroups)
	f, err := zipWriter.Create("recipients.csv")
	if ok := c.handleErrors(g, err); !ok {
		return false
	}
	writer := csv.NewWriter(f)
	if err := writer.Write(headers); err != nil {
		return c.handleErrors(g, err)
	}
	err = c.RecipientService.EachExportPage(ctx, session, companyID, func(recipients []*model.Recipient) error {
		for _, recipient := range recipients {
			groups, _ := recipient.Groups.Get()
			row := recipientExportRow(recipient, groups)
			if err := writer.Write(row); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return c.handleErrors(g, err)
	}
	writer.Flush()
	return c.handleErrors(g, writer.Error())
}

// addCampaignEventsToZip adds one events file per campaign of exactly the
// company, or per shared campaign when companyID is nil. The events are read
// one row at a time and written straight into the zip entry. An event with no
// recipient, as in an anonymous campaign, or whose recipient is gone, is
// written with anonymized in place of the name and email. It returns false
// when a response was written.
func (c *Company) addCampaignEventsToZip(
	g *gin.Context,
	session *model.Session,
	companyID *uuid.UUID,
	zipWriter *zip.Writer,
) bool {
	campaigns, err := c.CampaignService.GetByCompanyID(
		g,
		session,
		companyID,
		&repository.CampaignOption{},
	)
	if ok := c.handleErrors(g, err); !ok {
		return false
	}
	desc := g.DefaultQuery("sortOrder", "desc") == "desc"
	usedZipNames := map[string]bool{}
	for _, campaign := range campaigns.Rows {
		headers := []string{
			"Campaign",
			"Created at",
			"Recipient first name",
			"Recipient last name",
			"Recipient email",
			"Event name",
			"Event Details",
			"User-Agent",
			"IP",
		}
		campaignName := utils.CSVRemoveFormulaStart(campaign.Name.MustGet().String())
		filename := uniqueZipName(usedZipNames, "campaign_events/", sanitizeFilename(campaign.Name.MustGet().String()), ".csv")
		f, err := zipWriter.Create(filename)
		if ok := c.handleErrors(g, err); !ok {
			return false
		}
		writer := csv.NewWriter(f)
		if err := writer.Write(headers); err != nil {
			return c.handleErrors(g, err)
		}
		cid := campaign.ID.MustGet()
		err = c.CampaignService.EachEventForExport(g.Request.Context(), session, &cid, desc, func(event *repository.CampaignEventExportRow) error {
			firstName, lastName, email := "anonymized", "anonymized", "anonymized"
			if event.HasRecipient {
				firstName, lastName, email = event.FirstName, event.LastName, event.Email
			}
			return writer.Write([]string{
				campaignName,
				utils.CSVFromDate(&event.CreatedAt),
				utils.CSVRemoveFormulaStart(firstName),
				utils.CSVRemoveFormulaStart(lastName),
				utils.CSVRemoveFormulaStart(email),
				utils.CSVRemoveFormulaStart(cache.EventNameByID[event.EventID.String()]),
				utils.CSVRemoveFormulaStart(event.Data),
				utils.CSVRemoveFormulaStart(event.UserAgent),
				utils.CSVRemoveFormulaStart(event.IP),
			})
		})
		if err != nil {
			return c.handleErrors(g, err)
		}
		writer.Flush()
		if ok := c.handleErrors(g, writer.Error()); !ok {
			return false
		}
	}
	return true
}

// exportZip is a zip written to a temporary file, so a large export does not
// grow the memory of the server, and a failure part way answers with an error
// instead of a partial file
type exportZip struct {
	*zip.Writer
	file *os.File
	// unlinked is set when the file name was removed right after the file was
	// created, the open file is then freed when it is closed
	unlinked bool
}

// newExportZip creates the temporary file of an export in the temporary
// directory of the system. The file is readable by the server user only. Its
// name is removed at once and the open handle is used from then on, so a
// crash leaves no file behind. It returns false when a response was written.
func (c *Company) newExportZip(g *gin.Context) (*exportZip, bool) {
	file, err := os.CreateTemp("", "phishingclub-export-*.zip")
	if err != nil {
		c.Logger.Errorw("failed to create export file", "error", err)
		return nil, c.handleErrors(g, err)
	}
	// a system that can not remove an open file keeps the name until remove
	unlinked := os.Remove(file.Name()) == nil
	return &exportZip{Writer: zip.NewWriter(file), file: file, unlinked: unlinked}, true
}

// remove closes the temporary file, and deletes it when its name is still there
func (z *exportZip) remove() {
	_ = z.file.Close()
	if !z.unlinked {
		_ = os.Remove(z.file.Name())
	}
}

// responseWithExportZip completes the zip and sends the file. It reports
// whether the file was complete and its sending started.
func (c *Company) responseWithExportZip(g *gin.Context, z *exportZip, name string) bool {
	if ok := c.handleErrors(g, z.Close()); !ok {
		return false
	}
	size, err := z.file.Seek(0, io.SeekCurrent)
	if ok := c.handleErrors(g, err); !ok {
		return false
	}
	if _, err := z.file.Seek(0, io.SeekStart); err != nil {
		c.handleErrors(g, err)
		return false
	}
	g.Header("Content-Type", "application/zip")
	setSecureContentDisposition(g, name)
	g.Header("Content-Transfer-Encoding", "binary")
	g.Header("Expires", "0")
	g.Header("Cache-Control", "must-revalidate")
	g.Header("Pragma", "public")
	g.Header("Content-Length", fmt.Sprintf("%d", size))
	g.Status(http.StatusOK)
	if _, err := io.Copy(g.Writer, z.file); err != nil {
		// the headers are sent, so the client sees a short download
		c.Logger.Errorw("failed to send export", "error", err)
		return false
	}
	return true
}

// uniqueZipName returns dir + base + ext, or with " (2)", " (3)" and so on
// after base when that name is already used, so two entries never share a
// name. Each final name is checked, also against a name that already ends in
// a number, and is added to used.
func uniqueZipName(used map[string]bool, dir, base, ext string) string {
	name := dir + base + ext
	for n := 2; used[strings.ToLower(name)]; n++ {
		name = fmt.Sprintf("%s%s (%d)%s", dir, base, n, ext)
	}
	used[strings.ToLower(name)] = true
	return name
}

// addProgramEventsToZip adds programs.csv, listing the programs of exactly the
// company (global programs when companyID is nil), and one events file per
// program that is not anonymous. An anonymous program has no events file, it
// gives no per person data. It returns false when a response was written.
func (c *Company) addProgramEventsToZip(
	g *gin.Context,
	session *model.Session,
	companyID *uuid.UUID,
	zipWriter *zip.Writer,
) bool {
	programs, err := c.CampaignService.GetProgramEventsExport(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return false
	}
	// the same order as the campaign event files
	desc := g.DefaultQuery("sortOrder", "desc") == "desc"
	summary := &bytes.Buffer{}
	summaryWriter := csv.NewWriter(summary)
	if err := summaryWriter.Write([]string{"Program", "Anonymous", "Events file"}); err != nil {
		return c.handleErrors(g, err)
	}
	usedNames := map[string]bool{}
	for _, program := range programs {
		eventsFile := ""
		if !program.Anonymous {
			eventsFile = uniqueZipName(usedNames, "program_events/", sanitizeProgramFilename(program.ProgramName), ".csv")
		}
		anonymous := "no"
		if program.Anonymous {
			anonymous = "yes"
		}
		if err := summaryWriter.Write([]string{
			utils.CSVRemoveFormulaStart(program.ProgramName),
			anonymous,
			utils.CSVRemoveFormulaStart(eventsFile),
		}); err != nil {
			return c.handleErrors(g, err)
		}
		if eventsFile == "" {
			continue
		}
		// the rows are written straight into the zip as they are read, so the
		// events of a program are never held in memory
		f, err := zipWriter.Create(eventsFile)
		if ok := c.handleErrors(g, err); !ok {
			return false
		}
		writer := csv.NewWriter(f)
		if err := writer.Write([]string{
			"Program",
			"Template",
			"Created at",
			"Recipient first name",
			"Recipient last name",
			"Recipient email",
			"Event name",
			"Event Details",
			"User-Agent",
			"IP",
		}); err != nil {
			return c.handleErrors(g, err)
		}
		err = program.EachEvent(g.Request.Context(), desc, func(event *repository.ProgramEventExportRow) error {
			firstName, lastName, email := "anonymized", "anonymized", "anonymized"
			if event.HasRecipient {
				firstName, lastName, email = event.FirstName, event.LastName, event.Email
			}
			return writer.Write([]string{
				utils.CSVRemoveFormulaStart(program.ProgramName),
				utils.CSVRemoveFormulaStart(program.TemplateByCampaign[event.CampaignID]),
				utils.CSVFromDate(&event.CreatedAt),
				utils.CSVRemoveFormulaStart(firstName),
				utils.CSVRemoveFormulaStart(lastName),
				utils.CSVRemoveFormulaStart(email),
				utils.CSVRemoveFormulaStart(cache.EventNameByID[event.EventID.String()]),
				utils.CSVRemoveFormulaStart(event.Data),
				utils.CSVRemoveFormulaStart(event.UserAgent),
				utils.CSVRemoveFormulaStart(event.IP),
			})
		})
		if err != nil {
			return c.handleErrors(g, err)
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return c.handleErrors(g, err)
		}
	}
	summaryWriter.Flush()
	f, err := zipWriter.Create("programs.csv")
	if ok := c.handleErrors(g, err); !ok {
		return false
	}
	if _, err := f.Write(summary.Bytes()); err != nil {
		return c.handleErrors(g, err)
	}
	return true
}

// ExportShared sends a zip of CSV files with the shared recipients, the
// events of each shared campaign and the events of each global program
func (c *Company) ExportShared(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleExportAuthorization(g, session, nil) {
		return
	}
	// the export holds recipients and campaign events, so it is audited
	done, err := c.CompanyService.AuthorizeExport(g.Request.Context(), session, nil)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// the event is written once the file is built and sent, or as failed
	built := false
	defer func() { done(built) }()
	// the zip is written to a temporary file and sent once it is complete
	zipWriter, ok := c.newExportZip(g)
	if !ok {
		return
	}
	defer zipWriter.remove()
	zipFileName := fmt.Sprintf("shared_export_%s.zip", time.Now().UTC().Format("2006-01-02"))
	if ok := c.addRecipientsToZip(g, session, nil, zipWriter.Writer); !ok {
		return
	}
	if ok := c.addCampaignEventsToZip(g, session, nil, zipWriter.Writer); !ok {
		return
	}
	// program sends of global programs, named by program and template
	if ok := c.addProgramEventsToZip(g, session, nil, zipWriter.Writer); !ok {
		return
	}
	built = c.responseWithExportZip(g, zipWriter, zipFileName)
}

// handleExportAuthorization checks the session holds the global permission
// before an export reads anything. A refused export is still written to the
// audit log as denied by the service, as an export holds recipients and
// campaign events. On a server error or a refusal it writes the response and
// returns false.
func (c *Company) handleExportAuthorization(g *gin.Context, session *model.Session, companyID *uuid.UUID) bool {
	isAuthorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		_ = handleServerError(g, c.Response, err)
		return false
	}
	if !isAuthorized {
		_, _ = c.CompanyService.AuthorizeExport(g.Request.Context(), session, companyID)
		c.Response.Unauthorized(g)
		return false
	}
	return true
}

// ChangeName changes a company name
func (c *Company) ChangeName(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.Company
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// change company name
	err := c.CompanyService.UpdateByID(
		g,
		session,
		id,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, nil)
}

// SoftDelete soft deletes a company
func (c *Company) DeleteByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// the delete is refused while the company holds rows it does not remove,
	// else it is marked and finished in the background
	_, err := c.CompanyService.DeleteByID(g.Request.Context(), session, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Create creates a company
func (c *Company) Create(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.Company
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// save company
	ctx := g.Request.Context()
	company, err := c.CompanyService.Create(
		ctx,
		session,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{
		"id": company.ID,
	})
}

// GetAll gets all companies with pagination
func (c *Company) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(CompanyColumnsMap)
	// get companies
	ctx := g.Request.Context()
	companies, err := c.CompanyService.GetAll(
		ctx,
		session,
		queryArgs,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, companies)
}
