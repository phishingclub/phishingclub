package controller

import (
	"github.com/gin-gonic/gin"
)

// GetTimeline returns the schedule timeline of a program. from and to are
// optional YYYY-MM-DD dates in the program zone, the service checks and bounds
// them. The optional companyID is the company in view.
func (c *Program) GetTimeline(g *gin.Context) {
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
	companyID, ok := c.programCompanyIDQuery(g)
	if !ok {
		return
	}
	timeline, err := c.ProgramService.GetTimeline(
		g.Request.Context(), session, id, companyID, g.Query("from"), g.Query("to"),
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, timeline)
}
