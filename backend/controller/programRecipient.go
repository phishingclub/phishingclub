package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/repository"
)

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
	queryArgs.RemapOrderBy(repository.ProgramEnrollmentSortColumns)
	view, err := c.ProgramService.GetRecipientsView(g.Request.Context(), session, id, queryArgs)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, view)
}

// GetRecipientPrograms returns one page of the programs a recipient is enrolled in.
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
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	// a sort name outside the allowlist falls back to the name
	queryArgs.RemapOrderBy(repository.RecipientProgramSortColumns)
	programs, err := c.ProgramService.GetRecipientPrograms(g.Request.Context(), session, id, queryArgs)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, programs)
}

// GetRecipientSchedule returns the plan of one recipient in a program
func (c *Program) GetRecipientSchedule(g *gin.Context) {
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
	recipientID, err := uuid.Parse(g.Param("recipientID"))
	if err != nil {
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return
	}
	schedule, err := c.ProgramService.GetRecipientSchedule(g.Request.Context(), session, id, &recipientID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, schedule)
}
