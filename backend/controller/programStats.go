package controller

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/repository"
)

// GetStats counts the programs per state for the dashboard. Without a
// company id it counts every company.
func (c *Program) GetStats(g *gin.Context) {
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
	stats, err := c.ProgramService.GetStats(g.Request.Context(), session, companyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, stats)
}

// GetCalendar lists the programs that run between the start and end query
// times for the dashboard calendar, a page at a time.
func (c *Program) GetCalendar(g *gin.Context) {
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
	start, err := time.Parse(time.RFC3339Nano, g.Query("start"))
	if err != nil {
		c.Response.ValidationFailed(g, "start", err)
		return
	}
	end, err := time.Parse(time.RFC3339Nano, g.Query("end"))
	if err != nil {
		c.Response.ValidationFailed(g, "end", err)
		return
	}
	entries, err := c.ProgramService.GetCalendarEntries(
		g.Request.Context(),
		session,
		companyID,
		start,
		end,
		&repository.ProgramOption{QueryArgs: queryArgs},
	)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, entries)
}
