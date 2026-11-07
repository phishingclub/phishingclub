package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// programColumns maps the frontend sort names to backend columns.
var programColumns = map[string]string{
	"created_at": repository.TableColumn(database.PROGRAM_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.PROGRAM_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.PROGRAM_TABLE, "name"),
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
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.RemapOrderBy(programColumns)
	queryArgs.DefaultSortByUpdatedAt()
	programs, err := c.ProgramService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.ProgramOption{QueryArgs: queryArgs},
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

// DeleteByID deletes a program.
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
	c.Response.OK(g, gin.H{})
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

// GetResults returns the program results by month and by template.
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
	results, err := c.ProgramService.GetResults(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, results)
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

// GetResultStats returns the program result funnel.
func (c *Program) GetResultStats(g *gin.Context) {
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
	stats, err := c.ProgramService.GetResultStats(g.Request.Context(), session, id)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, stats)
}

// GetRecipients returns the enrollment rows of a program.
func (c *Program) GetRecipients(g *gin.Context) {
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
	// risk is derived, it is sorted by the service rather than by a column
	if queryArgs.OrderBy != service.ProgramRiskSortKey {
		queryArgs.RemapOrderBy(repository.ProgramEnrollmentSortColumns)
	}
	view, err := c.ProgramService.GetRecipientsView(g.Request.Context(), session, id, queryArgs)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, view)
}
