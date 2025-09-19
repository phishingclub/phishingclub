package controller

import (
	"bytes"
	"encoding/csv"
	"io"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/build"
	"github.com/phishingclub/phishingclub/cache"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/embedded"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
)

// allowedCampaignColumns is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var allowedCampaignColumns = map[string]string{
	"created_at":    repository.TableColumn(database.CAMPAIGN_TABLE, "created_at"),
	"updated_at":    repository.TableColumn(database.CAMPAIGN_TABLE, "updated_at"),
	"closed_at":     repository.TableColumn(database.CAMPAIGN_TABLE, "closed_at"),
	"close_at":      repository.TableColumn(database.CAMPAIGN_TABLE, "close_at"),
	"anonymized_at": repository.TableColumn(database.CAMPAIGN_TABLE, "anonymized_at"),
	"is_test":       repository.TableColumn(database.CAMPAIGN_TABLE, "is_test"),
	"send_start_at": repository.TableColumn(database.CAMPAIGN_TABLE, "send_start_at"),
	"send_end_at":   repository.TableColumn(database.CAMPAIGN_TABLE, "send_end_at"),
	"template":      repository.TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "name"),
	"name":          repository.TableColumn(database.CAMPAIGN_TABLE, "name"),
}

// campaignEventColumns is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var campaignEventColumns = map[string]string{
	"created_at": repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "updated_at"),
	"details":    repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "data"),
	"ip":         repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "ip_address"),
	"user-agent": repository.TableColumn(database.CAMPAIGN_EVENT_TABLE, "user_agent"),
	"email":      repository.TableColumn(database.RECIPIENT_TABLE, "email"),
	"first_name": repository.TableColumn(database.RECIPIENT_TABLE, "first_name"),
	"last_name":  repository.TableColumn(database.RECIPIENT_TABLE, "last_name"),
	"event":      repository.TableColumn(database.EVENT_TABLE, "name"),
}

// allowedCampaignRecipientColumns is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var allowedCampaignRecipientColumns = map[string]string{
	"created_at":   "campaign_recipients.created_at",
	"updated_at":   "campaign_recipients.updated_at",
	"send_at":      "campaign_recipients.send_at",
	"sent_at":      "campaign_recipients.sent_at",
	"cancelled_at": "campaign_recipients.cancelled_at",
	"status":       "campaign_recipients.notable_event_id",
	"first_name":   "recipients.first_name",
	"last_name":    "recipients.last_name",
	"email":        "recipients.email",
}

// Campaign is a Campaign controller
type Campaign struct {
	Common
	CampaignService *service.Campaign
}

// CloseCampaignByID closes campaign
func (c *Campaign) CloseCampaignByID(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// close campaigns
	err := c.CampaignService.CloseCampaignByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle responses
	if errors.Is(err, errs.ErrCampaignAlreadyClosed) {
		c.Response.ValidationFailed(g, "", err)
		return
	}
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Create creates a new campaign
func (c *Campaign) Create(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.Campaign
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// create and schedule the campaign
	id, err := c.CampaignService.Create(g.Request.Context(), session, &req)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{
		"id": id.String(),
	})
}

// GetAllEventTypes gets all event  types
func (c *Campaign) GetAllEventTypes(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// check permissions
	isAuthorized, err := service.IsAuthorized(session, data.PERMISSION_ALLOW_GLOBAL)
	if err != nil && !errors.Is(err, errs.ErrAuthorizationFailed) {
		_ = handleServerError(g, c.Response, err)
		return
	}
	if !isAuthorized {
		c.Response.Unauthorized(g)
		return
	}
	// get all event names
	// we pick them out from the in memory cache
	ev := []gin.H{}
	for name, id := range cache.EventIDByName {
		ev = append(ev, gin.H{
			"id":   id,
			"name": name,
		})
	}
	c.Response.OK(g, ev)
}

// GetByID gets a campaign by its id
func (c *Campaign) GetByID(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get the campaign that needs to be updated
	campaign, err := c.CampaignService.GetByID(
		g.Request.Context(),
		session,
		id,
		&repository.CampaignOption{
			WithRecipientGroups: true,
			WithAllowDeny:       true,
			WithDenyPage:        true,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaign)
}

// GetByName gets a campaign by name
func (c *Campaign) GetByName(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	name := g.Param("name")
	if !ok {
		return
	}
	// get the campaign that needs to be updated
	campaign, err := c.CampaignService.GetByName(
		g,
		session,
		name,
		companyID,
		&repository.CampaignOption{
			WithRecipientGroups: true,
			WithAllowDeny:       true,
			WithDenyPage:        true,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaign)
}

// GetResultStats get campaign result stats
func (c *Campaign) GetResultStats(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get
	stats, err := c.CampaignService.GetResultStats(
		g.Request.Context(),
		session,
		id,
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, stats)
}

// GetCampaignStats get campaign stats
// if no company id is provided it gets the global stats including all companies
func (c *Campaign) GetStats(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	includeTestCampaigns := g.Query("includeTest") == "true"
	// get
	stats, err := c.CampaignService.GetStats(
		g.Request.Context(),
		session,
		companyID,
		includeTestCampaigns,
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, stats)
}

// GetAll gets all campaigns with pagination
func (c *Campaign) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(allowedCampaignColumns)
	queryArgs.DefaultSortByUpdatedAt()
	// get all campaigns
	campaigns, err := c.CampaignService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.CampaignOption{
			QueryArgs:            queryArgs,
			WithCampaignTemplate: true,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaigns)

}

// GetAll gets all campaigns within dates
func (c *Campaign) GetAllWithinDates(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	includeTestCampaigns := g.Query("includeTest") == "true"
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(allowedCampaignColumns)
	queryArgs.DefaultSortByUpdatedAt()
	// get start and end date for query
	startDate, err := time.Parse(time.RFC3339Nano, g.Query("start"))
	if err != nil {
		c.Response.ValidationFailed(g, "start", err)
		return
	}
	endDate, err := time.Parse(time.RFC3339Nano, g.Query("end"))
	if err != nil {
		c.Response.ValidationFailed(g, "end", err)
		return
	}
	// get all campaigns
	campaigns, err := c.CampaignService.GetAllWithinDates(
		g.Request.Context(),
		session,
		startDate,
		endDate,
		companyID,
		&repository.CampaignOption{
			QueryArgs:            queryArgs,
			WithCampaignTemplate: true,
			IncludeTestCampaigns: includeTestCampaigns,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaigns)

}

// GetAllActive gets all active campaigns with pagination
// if no company id is given it gets all globals including company
func (c *Campaign) GetAllActive(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	includeTestCampaigns := g.Query("includeTest") == "true"
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(allowedCampaignColumns)
	if queryArgs.OrderBy == "" {
		queryArgs.OrderBy = "send_start_at"
		queryArgs.Desc = false
	}
	// get all campaigns
	campaigns, err := c.CampaignService.GetAllActive(
		g.Request.Context(),
		session,
		companyID,
		&repository.CampaignOption{
			QueryArgs:            queryArgs,
			WithCompany:          true,
			WithCampaignTemplate: true,
			IncludeTestCampaigns: includeTestCampaigns,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaigns)
}

// GetAllUpcoming gets all upcoming campaigns with pagination
// if no company id is given it gets all globals including company
func (c *Campaign) GetAllUpcoming(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	includeTestCampaigns := g.Query("includeTest") == "true"
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(allowedCampaignColumns)
	if queryArgs.OrderBy == "" {
		queryArgs.OrderBy = "send_start_at"
		queryArgs.Desc = false
	}
	// get all campaigns
	campaigns, err := c.CampaignService.GetAllUpcoming(
		g.Request.Context(),
		session,
		companyID,
		&repository.CampaignOption{
			QueryArgs:            queryArgs,
			WithCompany:          true,
			WithCampaignTemplate: true,
			IncludeTestCampaigns: includeTestCampaigns,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaigns)
}

// GetAllFinished gets all finished campaigns with pagination
// if no company id is given it gets all globals including company
func (c *Campaign) GetAllFinished(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	includeTestCampaigns := g.Query("includeTest") == "true"
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(allowedCampaignColumns)
	if queryArgs.OrderBy == "" {
		queryArgs.OrderBy = "send_start_at"
		queryArgs.Desc = true
	}
	// get all campaigns
	campaigns, err := c.CampaignService.GetAllFinished(
		g.Request.Context(),
		session,
		companyID,
		&repository.CampaignOption{
			QueryArgs:            queryArgs,
			WithCompany:          true,
			WithCampaignTemplate: true,
			IncludeTestCampaigns: includeTestCampaigns,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaigns)
}

// GetEventsByCampaignID gets events by campaign id
func (c *Campaign) GetEventsByCampaignID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	// remap query args
	queryArgs.RemapOrderBy(campaignEventColumns)
	// set default sort order to desc
	sortOrder := g.DefaultQuery("sortOrder", "desc")
	if sortOrder == "desc" {
		queryArgs.Desc = true
	}
	var since *time.Time
	s, err := time.Parse(time.RFC3339Nano, g.Query("since"))
	if err == nil {
		since = &s
	}
	// get events by campaign id
	events, err := c.CampaignService.GetEventsByCampaignID(
		g.Request.Context(),
		session,
		id,
		queryArgs,
		since,
		nil,
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, events)
}

// ExportEventsAsCSV exports a all campaign events as a CSV
func (c *Campaign) ExportEventsAsCSV(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByCreatedAt()
	queryArgs.RemapOrderBy(campaignEventColumns)
	sortOrder := g.DefaultQuery("sortOrder", "desc")
	if sortOrder == "desc" {
		queryArgs.Desc = true
	}
	// get all rows
	queryArgs.Limit = 0
	queryArgs.Offset = 0
	// get events by campaign id
	events, err := c.CampaignService.GetEventsByCampaignID(
		g.Request.Context(),
		session,
		id,
		queryArgs,
		nil,
		nil,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}

	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)

	headers := []string{
		"Created at",
		"Recipient name",
		"Recipient email",
		"Event name",
		"Event Details",
		"User-Agent",
		"IP",
	}
	err = writer.Write(headers)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	for _, event := range events.Rows {
		row := []string{}
		// if the recipient has been deleted or anonymized
		if event.Recipient == nil {
			row = []string{
				utils.CSVFromDate(event.CreatedAt),
				"anonymized",
				"anonymized",
				utils.CSVRemoveFormulaStart(cache.EventNameByID[event.EventID.String()]),
				utils.CSVRemoveFormulaStart(event.Data.String()),
				utils.CSVRemoveFormulaStart(event.UserAgent.String()),
				utils.CSVRemoveFormulaStart(event.IP.String()),
			}
		} else {
			row = []string{
				utils.CSVFromDate(event.CreatedAt),
				utils.CSVRemoveFormulaStart(event.Recipient.FirstName.MustGet().String()),
				utils.CSVRemoveFormulaStart(event.Recipient.LastName.MustGet().String()),
				utils.CSVRemoveFormulaStart(event.Recipient.Email.MustGet().String()),
				utils.CSVRemoveFormulaStart(cache.EventNameByID[event.EventID.String()]),
				utils.CSVRemoveFormulaStart(event.Data.String()),
				utils.CSVRemoveFormulaStart(event.UserAgent.String()),
				utils.CSVRemoveFormulaStart(event.IP.String()),
			}
		}
		err = writer.Write(row)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	c.responseWithCSV(g, buffer, writer, "campaign_events.csv")
}

// ExportSubmissionsAsCSV exports all campaign submissions as a CSV
func (c *Campaign) ExportSubmissionsAsCSV(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByCreatedAt()
	queryArgs.RemapOrderBy(campaignEventColumns)
	sortOrder := g.DefaultQuery("sortOrder", "desc")
	if sortOrder == "desc" {
		queryArgs.Desc = true
	}
	// get all rows
	queryArgs.Limit = 0
	queryArgs.Offset = 0

	// filter for submission events only
	submissionEventID := cache.EventIDByName[data.EVENT_CAMPAIGN_RECIPIENT_SUBMITTED_DATA]
	eventTypeFilter := []string{submissionEventID.String()}

	// get submission events by campaign id
	events, err := c.CampaignService.GetEventsByCampaignID(
		g.Request.Context(),
		session,
		id,
		queryArgs,
		nil,
		eventTypeFilter,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}

	buffer := &bytes.Buffer{}
	writer := csv.NewWriter(buffer)

	headers := []string{
		"Submitted at",
		"Recipient first name",
		"Recipient last name",
		"Recipient email",
		"Submitted data",
		"User-Agent",
		"IP",
	}
	err = writer.Write(headers)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	for _, event := range events.Rows {
		row := []string{}
		// if the recipient has been deleted or anonymized
		if event.Recipient == nil {
			row = []string{
				utils.CSVFromDate(event.CreatedAt),
				"anonymized",
				"anonymized",
				"anonymized",
				utils.CSVRemoveFormulaStart(event.Data.String()),
				utils.CSVRemoveFormulaStart(event.UserAgent.String()),
				utils.CSVRemoveFormulaStart(event.IP.String()),
			}
		} else {
			row = []string{
				utils.CSVFromDate(event.CreatedAt),
				utils.CSVRemoveFormulaStart(event.Recipient.FirstName.MustGet().String()),
				utils.CSVRemoveFormulaStart(event.Recipient.LastName.MustGet().String()),
				utils.CSVRemoveFormulaStart(event.Recipient.Email.MustGet().String()),
				utils.CSVRemoveFormulaStart(event.Data.String()),
				utils.CSVRemoveFormulaStart(event.UserAgent.String()),
				utils.CSVRemoveFormulaStart(event.IP.String()),
			}
		}
		err = writer.Write(row)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}
	c.responseWithCSV(g, buffer, writer, "campaign_submissions.csv")
}

func (c *Campaign) GetCampaignEmail(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get email
	email, err := c.CampaignService.GetCampaignEmailBody(
		g.Request.Context(),
		session,
		id,
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, email)
}

// GetCampaignURL gets a recipient landing page URL
func (c *Campaign) GetCampaignURL(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	url, err := c.CampaignService.GetLandingPageURLByCampaignRecipientID(
		g.Request.Context(),
		session,
		id,
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, url)
}

// GetRecipientsByCampaignID gets recipients by campaign id
func (c *Campaign) GetRecipientsByCampaignID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// endpoints is handled a bit differently and allows to
	// fetch an unlimited amount of rows if no offset and limit is set.
	// TODO this endpoint should be changed to a Result<T> so we fetch the rows as needed.
	offset := g.DefaultQuery("offset", "")
	limit := g.DefaultQuery("limit", "")
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	// special case to retrieve ALL rows
	if offset == "" && limit == "" {
		queryArgs.Offset = 0
		queryArgs.Limit = 0
	}
	// remap query args
	queryArgs.DefaultSortBy("created_at")
	queryArgs.RemapOrderBy(allowedCampaignRecipientColumns)
	// get recipients by campaign id
	recipients, err := c.CampaignService.GetRecipientsByCampaignID(
		g.Request.Context(),
		session,
		id,
		&repository.CampaignRecipientOption{
			QueryArgs:     queryArgs,
			WithRecipient: true,
		},
	)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, recipients)
}

// TrackingPixel returns a tracking pixel
func (c *Campaign) TrackingPixel(g *gin.Context) {
	// get the campaign recipient id from the query
	campaignRecipientID := g.Query("upn") // expect the campaign recipient id to be in here
	if campaignRecipientID == "" {
		c.Response.NotFound(g)
		return
	}
	campaignRecipientUUID, err := uuid.Parse(campaignRecipientID)
	if err != nil {
		c.Logger.Debugw(errs.MsgFailedToParseRequest,
			"error", err,
		)
		c.Response.NotFound(g)
		return
	}
	err = c.CampaignService.SaveTrackingPixelLoaded(
		g,
		&campaignRecipientUUID,
	)
	if err != nil {
		c.Logger.Debugw("failed to save tracking pixel loaded event",
			"error", err,
		)
		c.Response.NotFound(g)
		return
	}
	g.Header("Content-Type", "image/gif")
	if !build.Flags.Production {
		g.File("./embedded/tracking-pixel/sendgrid/open.gif")
		return
	}
	_, err = g.Writer.Write(embedded.TrackingPixel)
	if err != nil {
		c.Logger.Errorw("failed to write tracking pixel", "error", err)
	}
}

// UpdateByID updates a campaign by its id
func (c *Campaign) UpdateByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}

	var req model.Campaign
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// update the campaign
	err := c.CampaignService.UpdateByID(g.Request.Context(), session, id, &req)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	// handle responses
	c.Response.OK(g, gin.H{})
}

// SetSentAtByCampaignRecipientID sets the sent at time for a campaign recipient
func (c *Campaign) SetSentAtByCampaignRecipientID(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// set sent at time
	err := c.CampaignService.SetSentAtByCampaignRecipientID(g.Request.Context(), session, id)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// SendEmailByCampaignRecipientID sends an email to a specific campaign recipient
func (c *Campaign) SendEmailByCampaignRecipientID(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// send message (email or API depending on campaign template configuration)
	err := c.CampaignService.SendEmailByCampaignRecipientID(g.Request.Context(), session, id)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// DeleteByID deletes a campaign by its id
func (c *Campaign) DeleteByID(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete
	err := c.CampaignService.DeleteByID(g, session, id)
	// handle responses
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// AnonymizeByID anonymizes a campaign by its id
func (c *Campaign) AnonymizeByID(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// anonymize
	err := c.CampaignService.AnonymizeByID(g, session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// GetCampaignStats gets campaign statistics by campaign ID
func (c *Campaign) GetCampaignStats(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// get stats
	stats, err := c.CampaignService.GetCampaignStats(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, stats)
}

// GetAllCampaignStats gets all campaign statistics with pagination
func (c *Campaign) GetAllCampaignStats(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(allowedCampaignColumns)
	companyID := companyIDFromRequestQuery(g)

	// get stats
	stats, err := c.CampaignService.GetAllCampaignStats(g.Request.Context(), session, queryArgs, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, stats)
}

// UploadReportedCSV uploads a CSV file with reported recipients
func (c *Campaign) UploadReportedCSV(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse campaign id
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}

	// get the uploaded file
	file, header, err := g.Request.FormFile("file")
	if err != nil {
		c.Response.ValidationFailed(g, "file", err)
		return
	}
	defer file.Close()

	// validate file extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".csv") {
		c.Response.ValidationFailed(g, "file", errors.New("file must be a CSV"))
		return
	}

	// read file content
	content, err := io.ReadAll(file)
	if err != nil {
		c.Response.ValidationFailed(g, "file", err)
		return
	}

	// parse CSV
	reader := csv.NewReader(strings.NewReader(string(content)))
	records, err := reader.ReadAll()
	if err != nil {
		c.Logger.Errorw("failed to parse CSV file", "error", err)
		c.Response.ValidationFailed(g, "file", errors.New("failed to parse CSV file: "+err.Error()))
		return
	}

	if len(records) < 2 {
		c.Logger.Debugw("CSV file has insufficient rows", "rows", len(records))
		c.Response.ValidationFailed(g, "file", errors.New("CSV file must have header and at least one data row"))
		return
	}

	c.Logger.Debugw("processing CSV", "rows", len(records), "headers", records[0])

	// process CSV
	processed, skipped, err := c.CampaignService.ProcessReportedCSV(g.Request.Context(), session, id, records)
	if err != nil {
		c.Logger.Errorw("failed to process reported CSV", "error", err)
		if ok := c.handleErrors(g, err); !ok {
			return
		}
	}

	c.Response.OK(g, gin.H{
		"processed": processed,
		"skipped":   skipped,
		"message":   "CSV processed successfully",
	})
}
