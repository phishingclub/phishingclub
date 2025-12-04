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
	"repeat_offender":  "is_repeat_offender", // Special case - don't use TableColumn
}

var recipientCampaignEventColumnMap = utils.MergeStringMaps(
	campaignEventColumns,
	map[string]string{
		"event":    repository.TableColumnName(database.EVENT_TABLE),
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
	recipientBuffer := &bytes.Buffer{}
	recipientWriter := csv.NewWriter(recipientBuffer)
	recpHeaders := []string{
		"Created at",
		"Updated at",
		"Email",
		"Phone",
		"Extra Identifier",
		"Name",
		"Position",
		"Department",
		"City",
		"Country",
		"Misc",
	}
	groups, _ := recp.Groups.Get()
	for i := range groups {
		recpHeaders = append(recpHeaders, fmt.Sprintf("Group %d", i+1))
	}
	err = recipientWriter.Write(recpHeaders)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
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
	}
	for _, group := range groups {
		row = append(row, group.Name.MustGet().String())
	}
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

	// parse request
	companyID := companyIDFromRequestQuery(g)

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
