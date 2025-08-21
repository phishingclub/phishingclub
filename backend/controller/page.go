package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// PageColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var PageColumnsMap = map[string]string{
	"created_at": repository.TableColumn(database.PAGE_TABLE, "created_at"),
	"updated_at": repository.TableColumn(database.PAGE_TABLE, "updated_at"),
	"name":       repository.TableColumn(database.PAGE_TABLE, "name"),
}

// Page is a Page controller
type Page struct {
	Common
	PageService     *service.Page
	TemplateService *service.Template
}

// Create creates a page
func (p *Page) Create(g *gin.Context) {
	// handle session
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	// parse req
	var req model.Page
	if ok := p.handleParseRequest(g, &req); !ok {
		return
	}
	// save page
	id, err := p.PageService.Create(
		g.Request.Context(),
		session,
		&req,
	)
	// handle response
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	p.Response.OK(
		g,
		gin.H{
			"id": id.String(),
		},
	)
}

// GetContentByID serves a page by id
func (p *Page) GetContentByID(g *gin.Context) {
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := p.handleParseIDParam(g)
	if !ok {
		return
	}
	// get page
	page, err := p.PageService.GetByID(
		g,
		session,
		id,
		&repository.PageOption{},
	)
	// handle response
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	content, err := page.Content.Get()
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	// build response
	phishingPage, err := p.TemplateService.ApplyPageMock(content.String())
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	p.Response.OK(g, phishingPage.String())
}

// GetAll gets pages using pagination
func (p *Page) GetAll(g *gin.Context) {
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := p.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	companyID := companyIDFromRequestQuery(g)
	// get pages
	pages, err := p.PageService.GetAll(
		g,
		session,
		companyID,
		&repository.PageOption{
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	p.Response.OK(g, pages)
}

// GetOverview gets pages overview using pagination
func (p *Page) GetOverview(g *gin.Context) {
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	// parse request
	queryArgs, ok := p.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	companyID := companyIDFromRequestQuery(g)
	// get pages
	pages, err := p.PageService.GetAll(
		g,
		session,
		companyID,
		&repository.PageOption{
			Fields:    []string{"id", "created_at", "updated_at", "name", "company_id"},
			QueryArgs: queryArgs,
		},
	)
	// handle response
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	p.Response.OK(g, pages)
}

// GetByID gets a page by id
func (p *Page) GetByID(g *gin.Context) {
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := p.handleParseIDParam(g)
	if !ok {
		return
	}
	// get page
	page, err := p.PageService.GetByID(
		g.Request.Context(),
		session,
		id,
		// do I really need to preload this?
		&repository.PageOption{
			WithCompany: true,
		},
	)
	// handle response
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	p.Response.OK(g, page)
}

// UpdateByID updates a page by id
func (p *Page) UpdateByID(g *gin.Context) {
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := p.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.Page
	if ok := p.handleParseRequest(g, &req); !ok {
		return
	}
	// update page
	err := p.PageService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&req,
	)
	// handle response
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	p.Response.OK(g, gin.H{})
}

// DeleteByID deletes a page by id
func (p *Page) DeleteByID(g *gin.Context) {
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := p.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete page
	err := p.PageService.DeleteByID(
		g.Request.Context(),
		session,
		id,
	)
	// handle response
	if ok := p.handleErrors(g, err); !ok {
		return
	}
	p.Response.OK(g, gin.H{})
}
