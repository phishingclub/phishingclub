package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/service"
)

// Import handles import for templates like emails, landing pages and so on
type Import struct {
	Common
	ImportService *service.Import
}

// Import imports a .zip file
func (im *Import) Import(g *gin.Context) {
	session, _, ok := im.handleSession(g)
	if !ok {
		return
	}
	// parse request
	f, err := g.FormFile("file")
	// handle responses
	if ok := im.handleErrors(g, err); !ok {
		return
	}

	// Read forCompany flag from form (treat "1" or "true" as true)
	forCompany := false
	if v := g.PostForm("forCompany"); v == "1" || v == "true" {
		forCompany = true
	}

	// Read companyID from form data if provided
	var companyID *uuid.UUID
	if companyIDStr := g.PostForm("companyID"); companyIDStr != "" {
		if cid, err := uuid.Parse(companyIDStr); err == nil {
			companyID = &cid
		}
	}

	summary, err := im.ImportService.Import(g, session, f, forCompany, companyID)
	if ok := im.handleErrors(g, err); !ok {
		return
	}
	im.Response.OK(g, summary)
}
