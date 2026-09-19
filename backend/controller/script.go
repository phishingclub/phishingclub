package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/script"
	"github.com/phishingclub/phishingclub/service"
)

// ScriptColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
var ScriptColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.SCRIPT_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.SCRIPT_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.SCRIPT_TABLE, "name"),
}

// Script is a controller
type Script struct {
	Common
	ScriptService *service.Script

	// Enabled mirrors config.ScriptServerConfig.Enabled. When false every
	// endpoint returns 404, matching the remote browser gate. The script
	// engine runs admin authored scripts with outbound network access, so it is
	// only enabled on instances where every operator is trusted as a server admin.
	Enabled bool
}

// isEnabled returns true when the feature is enabled, otherwise it aborts the
// request with 404 so the feature is invisible when turned off.
func (a *Script) isEnabled(g *gin.Context) bool {
	if !a.Enabled {
		g.AbortWithStatus(http.StatusNotFound)
		return false
	}
	return true
}

// Create creates a new script
func (a *Script) Create(g *gin.Context) {
	if !a.isEnabled(g) {
		return
	}
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.Script
	if ok := a.handleParseRequest(g, &req); !ok {
		return
	}
	// save script
	id, err := a.ScriptService.Create(g.Request.Context(), session, &req)
	// handle response
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(
		g,
		gin.H{
			"id": id.String(),
		},
	)
}

// GetAll gets the scripts
func (a *Script) GetAll(g *gin.Context) {
	if !a.isEnabled(g) {
		return
	}
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := a.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(ScriptColumnsMap)
	companyID := companyIDFromRequestQuery(g)
	// get
	scripts, err := a.ScriptService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.ScriptOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(
		g,
		scripts,
	)
}

// GetByID gets a script by id
func (a *Script) GetByID(g *gin.Context) {
	if !a.isEnabled(g) {
		return
	}
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// get
	script, err := a.ScriptService.GetByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, script)
}

// UpdateByID updates a script
func (a *Script) UpdateByID(g *gin.Context) {
	if !a.isEnabled(g) {
		return
	}
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}

	var req model.Script
	if ok := a.handleParseRequest(g, &req); !ok {
		return
	}
	// save
	err := a.ScriptService.Update(g.Request.Context(), session, id, &req)
	// handle response
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, nil)
}

// scriptTestRequest is the body for a test run: a script plus a simulated
// campaign event to feed it.
type scriptTestRequest struct {
	Script string `json:"script"`
	Event  struct {
		Name         string                 `json:"name"`
		CampaignName string                 `json:"campaignName"`
		Email        string                 `json:"email"`
		CampaignID   string                 `json:"campaignId"`
		RecipientID  string                 `json:"recipientId"`
		Data         map[string]interface{} `json:"data"`
	} `json:"event"`
}

// Test runs a script against a simulated event and returns what it did, without
// touching any campaign (log/info/emitEvent are captured, not applied).
func (a *Script) Test(g *gin.Context) {
	if !a.isEnabled(g) {
		return
	}
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	var req scriptTestRequest
	if ok := a.handleParseRequest(g, &req); !ok {
		return
	}
	result, err := a.ScriptService.Test(
		g.Request.Context(),
		session,
		req.Script,
		script.EventContext{
			CampaignID:   req.Event.CampaignID,
			RecipientID:  req.Event.RecipientID,
			Event:        req.Event.Name,
			CampaignName: req.Event.CampaignName,
			Email:        req.Event.Email,
			Data:         req.Event.Data,
		},
	)
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, result)
}

// DeleteByID deletes a script by id
func (a *Script) DeleteByID(g *gin.Context) {
	if !a.isEnabled(g) {
		return
	}
	session, _, ok := a.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := a.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete
	err := a.ScriptService.DeleteByID(g, session, id)
	// handle response
	if ok := a.handleErrors(g, err); !ok {
		return
	}
	a.Response.OK(g, nil)
}
