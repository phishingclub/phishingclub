package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// IdentifierColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var IdentifierColumnsMap = map[string]string{
	"name": repository.TableColumn(database.IDENTIFIER_TABLE, "name"),
}

type Identifier struct {
	Common
	IdentifierService *service.Identifier
}

// GetAll gets all identifiers
func (c *Identifier) GetAll(g *gin.Context) {
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
	// get
	identifiers, err := c.IdentifierService.GetAll(
		g,
		session,
		&repository.IdentifierOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(
		g,
		identifiers,
	)
}
