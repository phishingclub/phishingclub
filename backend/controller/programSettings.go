package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
)

// programCloneRequest is the body of a program clone. A null companyID makes
// the copy a global program.
type programCloneRequest struct {
	Name      string     `json:"name"`
	CompanyID *uuid.UUID `json:"companyID"`
}

// Clone copies a program as a new draft, to the same or another company.
func (c *Program) Clone(g *gin.Context) {
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
	var req programCloneRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	name, err := vo.NewString64(req.Name)
	if err != nil {
		c.handleErrors(g, validate.WrapErrorWithField(err, "name"))
		return
	}
	result, err := c.ProgramService.Clone(g.Request.Context(), session, id, *name, req.CompanyID)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}
