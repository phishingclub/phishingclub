package controller

import (
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// programColumns maps the frontend sort names to backend columns.
var programColumns = map[string]string{
	"created_at": repository.TableColumn(database.PROGRAM_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.PROGRAM_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.PROGRAM_TABLE, "name"),
	"start_at":   repository.TableColumn(database.PROGRAM_TABLE, "start_at"),
	"started_at": repository.TableColumn(database.PROGRAM_TABLE, "started_at"),
	"paused_at":  repository.TableColumn(database.PROGRAM_TABLE, "paused_at"),
}

// programCompanyIDQuery reads the optional companyID query of a program
// request. A value that is not a uuid is answered with a bad request and ok
// false, so a typo never widens the scope to every company.
func (c *Program) programCompanyIDQuery(g *gin.Context) (*uuid.UUID, bool) {
	return strictCompanyIDQuery(g, c.Response)
}

// Program is the controller for programs.
type Program struct {
	Common
	ProgramService *service.Program
}

// Create creates a new program.
func (c *Program) Create(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	var req model.Program
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	id, err := c.ProgramService.Create(g.Request.Context(), session, &req)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"id": id.String()})
}

// GetAll lists programs.
func (c *Program) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	companyID, ok := c.programCompanyIDQuery(g)
	if !ok {
		return
	}
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(programColumns)
	queryArgs.DefaultSortByUpdatedAt()
	// status is one of model.ProgramListStatuses, empty lists every program
	status := g.Query("status")
	if status != "" && !slices.Contains(model.ProgramListStatuses, status) {
		c.Response.BadRequestMessage(g, "unknown program status")
		return
	}
	allCompanies := g.Query("allCompanies") == "true"
	programs, err := c.ProgramService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.ProgramOption{
			QueryArgs: queryArgs,
			Status:    status,
			// the dashboard lists the programs of every company with their company
			AllCompanies: allCompanies,
			WithCompany:  allCompanies,
		},
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, programs)
}

// GetByID gets a program with its steps and groups.
func (c *Program) GetByID(g *gin.Context) {
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
	program, err := c.ProgramService.GetByID(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, program)
}

// UpdateByID updates a program.
func (c *Program) UpdateByID(g *gin.Context) {
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
	var req model.Program
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	err := c.ProgramService.UpdateByID(g.Request.Context(), session, id, &req)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// DeleteByID starts the delete of a program. The program is marked as being
// deleted and stops sending before the answer, its campaigns and results are
// removed in the background.
func (c *Program) DeleteByID(g *gin.Context) {
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
	err := c.ProgramService.DeleteByID(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"deleting": true})
}

// Pause pauses a program.
func (c *Program) Pause(g *gin.Context) {
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
	err := c.ProgramService.Pause(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Resume resumes a program.
func (c *Program) Resume(g *gin.Context) {
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
	err := c.ProgramService.Resume(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Start starts a draft or scheduled program now.
func (c *Program) Start(g *gin.Context) {
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
	err := c.ProgramService.Start(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Close closes a program and keeps its results.
func (c *Program) Close(g *gin.Context) {
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
	err := c.ProgramService.Close(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Anonymize replaces the people of a closed program with pseudonyms in every
// result.
func (c *Program) Anonymize(g *gin.Context) {
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
	err := c.ProgramService.AnonymizeByID(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// AnonymizeData removes the submitted data, IP addresses, user agents and
// browser metadata of a program while the people stay linked.
func (c *Program) AnonymizeData(g *gin.Context) {
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
	err := c.ProgramService.AnonymizeDataByID(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

type programAudienceRequest struct {
	CompanyID         *uuid.UUID   `json:"companyID"`
	RecipientGroupIDs []*uuid.UUID `json:"recipientGroupIDs"`
}

// CountAudience returns how many people the given groups hold.
func (c *Program) CountAudience(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	var req programAudienceRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	count, err := c.ProgramService.CountAudience(g.Request.Context(), session, req.CompanyID, req.RecipientGroupIDs)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"people": count})
}

// GetReportDeliveryStatus tells whether automated program reports of the
// company in the companyID query, or the global scope without it, can be sent.
func (c *Program) GetReportDeliveryStatus(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	if !c.handleGlobalAuthorization(g, session) {
		return
	}
	companyID, ok := c.programCompanyIDQuery(g)
	if !ok {
		return
	}
	status, err := c.ProgramService.GetReportDeliveryStatus(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, status)
}

// GetEvents returns one page of the events of a program, sorted by time.
func (c *Program) GetEvents(g *gin.Context) {
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
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	// the service sorts the events of a program by time only
	queryArgs.DefaultSortByCreatedAt()
	queryArgs.RemapOrderBy(campaignEventColumns)
	if g.DefaultQuery("sortOrder", "desc") == "desc" {
		queryArgs.Desc = true
	}
	// the events of one recipient when recipientID is given
	var recipientID *uuid.UUID
	if v := g.Query("recipientID"); v != "" {
		rid, err := uuid.Parse(v)
		if err != nil {
			c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
			return
		}
		recipientID = &rid
	}
	view, err := c.ProgramService.GetEvents(g.Request.Context(), session, id, queryArgs, recipientID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, view)
}
