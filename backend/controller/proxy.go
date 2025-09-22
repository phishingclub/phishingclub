package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// ProxyColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var ProxyColumnsMap = map[string]string{
	"created_at":    repository.TableColumn(database.PROXY_TABLE, "created_at"),
	"updated_at":    repository.TableColumn(database.PROXY_TABLE, "updated_at"),
	"name":          repository.TableColumn(database.PROXY_TABLE, "name"),
	"target_domain": repository.TableColumn(database.PROXY_TABLE, "target_domain"),
}

// Proxy is a proxy controller
type Proxy struct {
	Common
	ProxyService *service.Proxy
}

// Create creates a proxy
func (m *Proxy) Create(g *gin.Context) {
	// handle session
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.Proxy
	if ok := m.handleParseRequest(g, &req); !ok {
		return
	}
	// save proxy
	id, err := m.ProxyService.Create(
		g.Request.Context(),
		session,
		&req,
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, map[string]string{
		"id": id.String(),
	})
}

// GetOverview gets proxies overview using pagination
func (m *Proxy) GetOverview(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := m.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	companyID := companyIDFromRequestQuery(g)
	// get proxies
	proxies, err := m.ProxyService.GetAllOverview(
		companyID,
		g,
		session,
		queryArgs,
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, proxies)
}

// GetAll gets all proxies using pagination
func (m *Proxy) GetAll(g *gin.Context) {
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := m.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	companyID := companyIDFromRequestQuery(g)
	// get proxies
	proxies, err := m.ProxyService.GetAll(
		g,
		session,
		companyID,
		&repository.ProxyOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, proxies)
}

// GetByID gets a proxy by ID
func (m *Proxy) GetByID(g *gin.Context) {
	// handle session
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	// get proxy
	proxy, err := m.ProxyService.GetByID(
		g.Request.Context(),
		session,
		id,
		&repository.ProxyOption{},
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, proxy)
}

// UpdateByID updates a proxy by ID
func (m *Proxy) UpdateByID(g *gin.Context) {
	// handle session
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.Proxy
	if ok := m.handleParseRequest(g, &req); !ok {
		return
	}
	// update proxy
	err := m.ProxyService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&req,
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, map[string]string{
		"message": "Proxy updated",
	})
}

// DeleteByID deletes a proxy by ID
func (m *Proxy) DeleteByID(g *gin.Context) {
	// handle session
	session, _, ok := m.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := m.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete proxy
	err := m.ProxyService.DeleteByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := m.handleErrors(g, err); !ok {
		return
	}
	m.Response.OK(g, map[string]string{
		"message": "Proxy deleted",
	})
}
