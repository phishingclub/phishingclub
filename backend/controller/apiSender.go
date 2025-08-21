package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// APISenderColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var APISenderColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.API_SENDER_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.API_SENDER_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.API_SENDER_TABLE, "name"),
}

// APISender is a API sender controller
type APISender struct {
	Common
	APISenderService *service.APISender
}

// Create creates a new api sender
func (a *APISender) Create(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.APISender
	if ok := a.handleParseRequest(g, &req); !ok {
		return
	}
	id, err := a.APISenderService.Create(g, session, &req)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{"id": id.String()})
}

// GetAll gets all api senders
func (a *APISender) GetAll(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := a.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(APISenderColumnsMap)
	apiSenders, err := a.APISenderService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		repository.APISenderOption{
			QueryArgs: queryArgs,
		},
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, apiSenders)
}

// GetAllOverview gets all api senders with limited data
func (a *APISender) GetAllOverview(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := a.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(APISenderColumnsMap)
	apiSenders, err := a.APISenderService.GetAllOverview(
		g.Request.Context(),
		session,
		companyID,
		repository.APISenderOption{
			QueryArgs: queryArgs,
		},
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, apiSenders)
}

// GetByID gets a api sender by ID
func (a *APISender) GetByID(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse reqeuest
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// get api sender
	apiSender, err := a.APISenderService.GetByID(
		g,
		session,
		id,
		&repository.APISenderOption{},
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, apiSender)
}

// Update updates a api sender
func (a *APISender) UpdateByID(g *gin.Context) {
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.APISender
	if ok := a.handleParseRequest(g, &req); !ok {
		return
	}
	err := a.APISenderService.UpdateByID(
		g,
		session,
		id,
		&req,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{})
}

// DeletebyID deletes a api sender by ID
func (a *APISender) DeleteByID(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	err := a.APISenderService.DeleteByID(
		g.Request.Context(),
		session,
		id,
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, gin.H{})
}

// SendTest sends a api request test and outputs the api sender and response
func (a *APISender) SendTest(g *gin.Context) {
	// handle session
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	data, err := a.APISenderService.SendTest(
		g.Request.Context(),
		session,
		id,
	)
	// output the error
	if err != nil {
		a.Response.BadRequestMessage(g, err.Error())
		return
	}
	a.Response.OK(g, data)
}
