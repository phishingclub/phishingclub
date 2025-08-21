package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// WebhookColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var WebhookColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.WEBHOOK_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.WEBHOOK_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.WEBHOOK_TABLE, "name"),
}

// Webhook is a controller
type Webhook struct {
	Common
	WebhookService *service.Webhook
}

// Create creates a new webhook
func (w *Webhook) Create(g *gin.Context) {
	session, _, ok := w.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.Webhook
	if ok := w.handleParseRequest(g, &req); !ok {
		return
	}
	// save webhook
	id, err := w.WebhookService.Create(g.Request.Context(), session, &req)
	// handle response
	if ok := w.handleErrors(g, err); !ok {
		return
	}
	w.Response.OK(
		g,
		gin.H{
			"id": id.String(),
		},
	)
}

// GetAll gets the webhooks
func (w *Webhook) GetAll(g *gin.Context) {
	session, _, ok := w.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := w.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	companyID := companyIDFromRequestQuery(g)
	// get
	webhooks, err := w.WebhookService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.WebhookOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := w.handleErrors(g, err); !ok {
		return
	}
	w.Response.OK(
		g,
		webhooks,
	)
}

// GetByID gets a webhook by id
func (w *Webhook) GetByID(g *gin.Context) {
	session, _, ok := w.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := w.handleParseIDParam(g)
	if !ok {
		return
	}
	// get
	webhook, err := w.WebhookService.GetByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := w.handleErrors(g, err); !ok {
		return
	}
	w.Response.OK(g, webhook)
}

// Update updates a webhook
func (w *Webhook) UpdateByID(g *gin.Context) {
	session, _, ok := w.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := w.handleParseIDParam(g)
	if !ok {
		return
	}

	var req model.Webhook
	if ok := w.handleParseRequest(g, &req); !ok {
		return
	}
	// save
	err := w.WebhookService.Update(g.Request.Context(), session, id, &req)
	// handle response
	if ok := w.handleErrors(g, err); !ok {
		return
	}
	w.Response.OK(g, nil)
}

// DeleteByID deletes a webhook by id
func (w *Webhook) DeleteByID(g *gin.Context) {
	session, _, ok := w.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := w.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete
	err := w.WebhookService.DeleteByID(g, session, id)
	// handle response
	if ok := w.handleErrors(g, err); !ok {
		return
	}
	w.Response.OK(g, nil)
}

// SendTest sends a test webhook
func (w *Webhook) SendTest(g *gin.Context) {
	session, _, ok := w.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := w.handleParseIDParam(g)
	if !ok {
		return
	}
	// send
	data, err := w.WebhookService.SendTest(g.Request.Context(), session, id)
	// handle response
	if ok := w.handleErrors(g, err); !ok {
		return
	}
	w.Response.OK(g, data)
}
