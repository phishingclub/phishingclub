package controller

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
)

// recipientColumnByMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var recipientColumnByMap = map[string]string{
	"created_at":       repository.TableColumn(database.RECIPIENT_TABLE, "created_at"),
	"updated_at":       repository.TableColumn(database.RECIPIENT_TABLE, "updated_at"),
	"email":            repository.TableColumn(database.RECIPIENT_TABLE, "email"),
	"phone":            repository.TableColumn(database.RECIPIENT_TABLE, "phone"),
	"extra identifier": repository.TableColumn(database.RECIPIENT_TABLE, "extra_identifier"),
	"first_name":       repository.TableColumn(database.RECIPIENT_TABLE, "first_name"),
	"last_name":        repository.TableColumn(database.RECIPIENT_TABLE, "last_name"),
	"position":         repository.TableColumn(database.RECIPIENT_TABLE, "position"),
	"department":       repository.TableColumn(database.RECIPIENT_TABLE, "department"),
	"city":             repository.TableColumn(database.RECIPIENT_TABLE, "city"),
	"country":          repository.TableColumn(database.RECIPIENT_TABLE, "country"),
	"misc":             repository.TableColumn(database.RECIPIENT_TABLE, "misc"),
	"language":         repository.TableColumn(database.RECIPIENT_TABLE, "language"),
	"repeat_offender":  "is_repeat_offender", // Special case - don't use TableColumn
}

var recipientCampaignEventColumnMap = utils.MergeStringMaps(
	campaignEventColumns,
	map[string]string{
		"event":    repository.TableColumn(database.EVENT_TABLE, "name"),
		"created":  repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"),
		"campaign": repository.TableColumn(database.CAMPAIGN_TABLE, "name"),
	},
)

// Recipient is a Recipient controller
type Recipient struct {
	Common
	RecipientService *service.Recipient
}

// Create inserts a new recipient
func (r *Recipient) Create(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.Recipient
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	// save recipient
	id, err := r.RecipientService.Create(
		g.Request.Context(),
		session,
		&req,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(
		g,
		gin.H{
			"id": id.String(),
		},
	)
}

// GetCampaignEvents gets all campaign events by recipient id and campaign id
// gets all events if campaign id is nil
func (r *Recipient) GetCampaignEvents(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	recipientID, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// optional param
	var campaignID *uuid.UUID
	cid, err := uuid.Parse(g.Query("campaignID"))
	if err == nil {
		campaignID = &cid
	}
	queryArgs, ok := r.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByCreatedAt()
	// remap query args
	queryArgs.RemapOrderBy(recipientCampaignEventColumnMap)
	// get events
	events, err := r.RecipientService.GetAllCampaignEvents(
		g.Request.Context(),
		session,
		recipientID,
		campaignID,
		queryArgs,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, events)
}

// recipientExportHeaders gives the header row of a recipient CSV export with
// one column per group
func recipientExportHeaders(groupCount int) []string {
	headers := []string{
		"Created at",
		"Updated at",
		"Email",
		"Phone",
		"Extra Identifier",
		"First name",
		"Last name",
		"Position",
		"Department",
		"City",
		"Country",
		"Misc",
		"Language",
	}
	for i := 1; i <= groupCount; i++ {
		headers = append(headers, fmt.Sprintf("Group %d", i))
	}
	return headers
}

// recipientExportRow gives the CSV row of a recipient in the same column
// order as recipientExportHeaders
func recipientExportRow(recp *model.Recipient, groups []*model.RecipientGroup) []string {
	row := []string{
		utils.CSVFromDate(recp.CreatedAt),
		utils.CSVFromDate(recp.UpdatedAt),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.Email)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.Phone)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.ExtraIdentifier)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.FirstName)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.LastName)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.Position)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.Department)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.City)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.Country)),
		utils.CSVRemoveFormulaStart(utils.NullableToString(recp.Misc)),
		utils.CSVRemoveFormulaStart(utils.NullableStringValue(recp.Language)),
	}
	for _, group := range groups {
		row = append(row, utils.CSVRemoveFormulaStart(group.Name.MustGet().String()))
	}
	return row
}

// Export outputs a zip with recipient, groups and all events related to the recipient
func (r *Recipient) Export(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	recipientID, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// get the recipient
	recp, err := r.RecipientService.GetByID(
		g,
		session,
		recipientID,
		&repository.RecipientOption{
			WithCompany: true,
			WithGroups:  true,
		},
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	// the export holds the recipient and their events, so it is audited
	if ok := r.handleErrors(g, r.RecipientService.AuthorizeExport(session, recipientID)); !ok {
		return
	}
	recipientBuffer := &bytes.Buffer{}
	recipientWriter := csv.NewWriter(recipientBuffer)
	groups, _ := recp.Groups.Get()
	recpHeaders := recipientExportHeaders(len(groups))
	err = recipientWriter.Write(recpHeaders)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	row := recipientExportRow(recp, groups)
	err = recipientWriter.Write(row)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	recipientWriter.Flush()

	queryArgs, ok := r.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByCreatedAt()
	// remap query args
	queryArgs.RemapOrderBy(recipientCampaignEventColumnMap)
	sortOrder := g.DefaultQuery("sortOrder", "desc")
	if sortOrder == "desc" {
		queryArgs.Desc = true
	}

	// get all rows
	queryArgs.Limit = 0
	queryArgs.Offset = 0
	// get events
	events, err := r.RecipientService.GetAllCampaignEvents(
		g.Request.Context(),
		session,
		recipientID,
		nil,
		queryArgs,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	// handle response
	eventsBuffer := &bytes.Buffer{}
	eventsWriter := csv.NewWriter(eventsBuffer)

	headers := []string{
		"Created at",
		"Campaign",
		"IP",
		"User-Agent",
		"Event Details",
		"Event",
	}
	err = eventsWriter.Write(headers)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	for _, event := range events.Rows {
		row := []string{}
		row = []string{
			utils.CSVFromDate(event.CreatedAt),
			utils.CSVRemoveFormulaStart(event.CampaignName),
			utils.CSVRemoveFormulaStart(event.IP.String()),
			utils.CSVRemoveFormulaStart(event.UserAgent.String()),
			utils.CSVRemoveFormulaStart(event.Data.String()),
			utils.CSVRemoveFormulaStart(cache.EventNameByID[event.EventID.String()]),
		}
		err = eventsWriter.Write(row)
		if ok := r.handleErrors(g, err); !ok {
			return
		}
	}
	eventsWriter.Flush()

	// program sends and events, named by program and template. an anonymous
	// program is left out, it never gives per person data
	programSends, programEvents, err := r.RecipientService.GetProgramExport(
		g.Request.Context(),
		session,
		recipientID,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	programSendsBuffer := &bytes.Buffer{}
	programSendsWriter := csv.NewWriter(programSendsBuffer)
	err = programSendsWriter.Write([]string{
		"Sent at",
		"Program",
		"Template",
		"Kind",
		"Most notable event",
		"Language",
	})
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	for _, send := range programSends {
		kind := service.ProgramSendKindPhishing
		if send.IsTraining {
			kind = service.ProgramSendKindTraining
		}
		notable := ""
		if send.NotableEventID != nil {
			notable = cache.EventNameByID[send.NotableEventID.String()]
		}
		err = programSendsWriter.Write([]string{
			utils.CSVFromDate(send.SentAt),
			utils.CSVRemoveFormulaStart(send.ProgramName),
			utils.CSVRemoveFormulaStart(send.TemplateName),
			kind,
			utils.CSVRemoveFormulaStart(notable),
			utils.CSVRemoveFormulaStart(send.EffectiveLanguage()),
		})
		if ok := r.handleErrors(g, err); !ok {
			return
		}
	}
	programSendsWriter.Flush()
	programEventsBuffer := &bytes.Buffer{}
	programEventsWriter := csv.NewWriter(programEventsBuffer)
	err = programEventsWriter.Write([]string{
		"Created at",
		"Program",
		"Template",
		"IP",
		"User-Agent",
		"Event Details",
		"Event",
	})
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	for _, event := range programEvents {
		eventName := ""
		if event.EventID != nil {
			eventName = cache.EventNameByID[event.EventID.String()]
		}
		err = programEventsWriter.Write([]string{
			utils.CSVFromDate(event.CreatedAt),
			utils.CSVRemoveFormulaStart(event.ProgramName),
			utils.CSVRemoveFormulaStart(event.TemplateName),
			utils.CSVRemoveFormulaStart(event.IPAddress),
			utils.CSVRemoveFormulaStart(event.UserAgent),
			utils.CSVRemoveFormulaStart(event.Data),
			utils.CSVRemoveFormulaStart(eventName),
		})
		if ok := r.handleErrors(g, err); !ok {
			return
		}
	}
	programEventsWriter.Flush()

	// create ZIP file in memory
	zipBuffer := new(bytes.Buffer)
	zipWriter := zip.NewWriter(zipBuffer)
	zipFileName := fmt.Sprintf("recipient_export_%s.zip", recp.Email.MustGet().String())

	// add events to zip
	{
		f, err := zipWriter.Create("recipient.csv")
		if ok := r.handleErrors(g, err); !ok {
			return
		}
		_, err = f.Write(recipientBuffer.Bytes())
		if ok := r.handleErrors(g, err); !ok {
			return
		}
	}
	// add events to zip
	{
		f, err := zipWriter.Create("events.csv")
		if ok := r.handleErrors(g, err); !ok {
			return
		}
		_, err = f.Write(eventsBuffer.Bytes())
		if ok := r.handleErrors(g, err); !ok {
			return
		}
	}
	// add program sends and events to zip
	for _, file := range []struct {
		name   string
		buffer *bytes.Buffer
	}{
		{"program_sends.csv", programSendsBuffer},
		{"program_events.csv", programEventsBuffer},
	} {
		f, err := zipWriter.Create(file.name)
		if ok := r.handleErrors(g, err); !ok {
			return
		}
		_, err = f.Write(file.buffer.Bytes())
		if ok := r.handleErrors(g, err); !ok {
			return
		}
	}
	// close zip
	err = zipWriter.Close()
	if ok := r.handleErrors(g, err); !ok {
		return
	}

	r.responseWithZIP(g, zipBuffer, zipFileName)
}

// GetRepeatOffenderCount gets the repeat offender count
func (r *Recipient) GetRepeatOffenderCount(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}

	// parse request, a bad companyID is a 400 and never means all companies
	companyID, ok := strictCompanyIDQuery(g, r.Response)
	if !ok {
		return
	}

	// get count
	count, err := r.RecipientService.GetRepeatOffenderCount(
		g.Request.Context(),
		session,
		companyID,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}

	r.Response.OK(g, count)
}

// GetOrphaned gets all recipients that are not in any group
func (r *Recipient) GetOrphaned(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := r.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortBy("first_name")
	// remap query args
	queryArgs.RemapOrderBy(recipientColumnByMap)
	// get orphaned recipients
	recipients, err := r.RecipientService.GetOrphaned(
		g.Request.Context(),
		companyID,
		session,
		&repository.RecipientOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, recipients)
}

// DeleteAllOrphaned deletes all recipients that are not in any group
func (r *Recipient) DeleteAllOrphaned(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	// delete orphaned recipients
	count, err := r.RecipientService.DeleteAllOrphaned(
		g.Request.Context(),
		companyID,
		session,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, gin.H{
		"count": count,
	})
}

// GetAll gets all recipients
func (r *Recipient) GetAll(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := r.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortBy("first_name")
	// remap query args
	queryArgs.RemapOrderBy(recipientColumnByMap)
	// get recipients
	recipients, err := r.RecipientService.GetAll(
		g.Request.Context(),
		companyID,
		session,
		&repository.RecipientOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, recipients)
}

// GetByID gets a recipient by id
func (r *Recipient) GetByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// get recipient
	recipient, err := r.RecipientService.GetByID(
		g.Request.Context(),
		session,
		id,
		&repository.RecipientOption{
			WithCompany: true,
			WithGroups:  true,
		},
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, recipient)
}

// GetStatsByID gets a recipient campaign stats by id
func (r *Recipient) GetStatsByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// get recipient stats
	stats, err := r.RecipientService.GetStatsByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, stats)
}

// UpdateByID updates a recipient by id
func (r *Recipient) UpdateByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.Recipient
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	err := r.RecipientService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&req,
	)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, gin.H{})
}

// Import imports recipients
func (r *Recipient) Import(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req struct {
		Recipients                 []*model.Recipient      `json:"recipients"`
		CompanyID                  *uuid.UUID              `json:"companyID"`
		IgnoreOverwriteEmptyFields nullable.Nullable[bool] `json:"ignoreOverwriteEmptyFields"`
	}
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	// IgnoreOverwriteEmptyFields default value is true
	if !req.IgnoreOverwriteEmptyFields.IsSpecified() || req.IgnoreOverwriteEmptyFields.IsNull() {
		req.IgnoreOverwriteEmptyFields = nullable.NewNullableWithValue(true)
	}
	result, err := r.RecipientService.Import(
		g,
		session,
		req.Recipients,
		req.IgnoreOverwriteEmptyFields.MustGet(),
		req.CompanyID,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, result)
}

// DeleteByID deletes a recipient by id
func (r *Recipient) DeleteByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	// parse id
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete recipient
	err := r.RecipientService.DeleteByID(g, session, id)
	// handle response
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, gin.H{})
}
