package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// AllowDenyColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var AllowDenyColumnsMap = map[string]string{
	"created_at":      repository.TableColumn(database.ALLOW_DENY_TABLE, "created_at"),
	"updated_at":      repository.TableColumn(database.ALLOW_DENY_TABLE, "updated_at"),
	"hosting_website": repository.TableColumn(database.ALLOW_DENY_TABLE, "host_website"),
	"redirects":       repository.TableColumn(database.ALLOW_DENY_TABLE, "redirect_url"),
}

// AllowDeny is a controller
type AllowDeny struct {
	Common
	AllowDenyService *service.AllowDeny
}

// Create creates a new AllowDeny
func (c *AllowDeny) Create(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.AllowDeny
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// save
	id, err := c.AllowDenyService.Create(g, session, &req)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		gin.H{
			"id": id.String(),
		},
	)
}

// GetAll gets AllowDenies
func (c *AllowDeny) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByName()
	companyID := companyIDFromRequestQuery(g)
	// get
	allowDenies, err := c.AllowDenyService.GetAll(
		g,
		session,
		companyID,
		&repository.AllowDenyOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		allowDenies,
	)
}

// GetAllOverview gets AllowDenies
func (c *AllowDeny) GetAllOverview(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByName()
	companyID := companyIDFromRequestQuery(g)
	allowDenies, err := c.AllowDenyService.GetAll(
		g,
		session,
		companyID,
		&repository.AllowDenyOption{
			Fields: []string{
				"id",
				"created_at",
				"updated_at",
				"company_id",
				"name",
				"allowed",
			},
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		allowDenies,
	)
}

// GetByID gets an AllowDeny by ID
func (c *AllowDeny) GetByID(g *gin.Context) {
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
	allowDeny, err := c.AllowDenyService.GetByID(
		g,
		session,
		id,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		allowDeny,
	)
}

// UpdateByID updates an AllowDeny
func (c *AllowDeny) UpdateByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.AllowDeny
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	if ok := c.handleParseRequest(g, &req); !ok {

		return
	}
	// update
	err := c.AllowDenyService.Update(g, session, id, &req)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		nil,
	)
}

// DeleteByID deletes an AllowDeny
func (c *AllowDeny) DeleteByID(g *gin.Context) {
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
	err := c.AllowDenyService.DeleteByID(g, session, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		nil,
	)
}
