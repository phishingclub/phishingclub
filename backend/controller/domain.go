package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"

	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/vo"
)

// DomainColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var DomainColumnsMap = map[string]string{
	"created_at":      repository.TableColumn(database.DOMAIN_TABLE, "created_at"),
	"updated_at":      repository.TableColumn(database.DOMAIN_TABLE, "updated_at"),
	"hosting_website": repository.TableColumn(database.DOMAIN_TABLE, "host_website"),
	"redirects":       repository.TableColumn(database.DOMAIN_TABLE, "redirect_url"),
}

// Domain
type Domain struct {
	Common
	DomainService *service.Domain
}

// Create creates a domain
func (d *Domain) Create(g *gin.Context) {
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.Domain
	if ok := d.handleParseRequest(g, &req); !ok {
		return
	}
	// save domain
	id, err := d.DomainService.Create(g, session, &req)
	// handle response
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(
		g,
		gin.H{
			"id": id,
		},
	)
}

// GetAll gets domains
func (d *Domain) GetAll(g *gin.Context) {
	// handle session
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := d.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(DomainColumnsMap)
	// get domain
	domains, err := d.DomainService.GetAll(
		companyID,
		g.Request.Context(),
		session,
		queryArgs,
		true, // TODO there might not be any reason to retrieve the full relation here - optimize by removing it (false)
	)
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(g, domains)
}

// GetAllOverview gets domains with limited data
func (d *Domain) GetAllOverview(g *gin.Context) {
	// handle session
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := d.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(DomainColumnsMap)
	// get domains
	domains, err := d.DomainService.GetAllOverview(
		companyID,
		g.Request.Context(),
		session,
		queryArgs,
	)
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(g, domains)
}

// GetAllOverviewWithoutProxies gets domains with limited data, excluding proxy domains for asset management
func (d *Domain) GetAllOverviewWithoutProxies(g *gin.Context) {
	// handle session
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := d.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(DomainColumnsMap)
	// get domains excluding proxy domains for asset management
	domains, err := d.DomainService.GetAllOverviewWithoutProxies(
		companyID,
		g.Request.Context(),
		session,
		queryArgs,
	)
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(g, domains)
}

// GetByID gets a domain by id
func (d *Domain) GetByID(g *gin.Context) {
	// handle session
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := d.handleParseIDParam(g)
	if !ok {
		return
	}
	// get domain
	ctx := g.Request.Context()
	domain, err := d.DomainService.GetByID(
		ctx,
		session,
		id,
		&repository.DomainOption{
			WithCompany: true,
		},
	)
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(g, domain)
}

// GetByName gets a domain by name
func (d *Domain) GetByName(g *gin.Context) {
	// handle session
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	name, err := vo.NewString255(g.Param("domain"))
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	// get domain
	ctx := g.Request.Context()
	domain, err := d.DomainService.GetByName(
		ctx,
		session,
		name,
		&repository.DomainOption{},
	)
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(g, domain)
}

// UpdateByID updates a domain by id
func (d *Domain) UpdateByID(g *gin.Context) {
	// handle session
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := d.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.Domain
	if ok := d.handleParseRequest(g, &req); !ok {
		return
	}
	// update domain
	err := d.DomainService.UpdateByID(
		g,
		session,
		id,
		&req,
	)
	// handle response
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(g, gin.H{})
}

// DeleteByID deletes a domain by id
func (d *Domain) DeleteByID(g *gin.Context) {
	// handle session
	session, _, ok := d.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := d.handleParseIDParam(g)
	if !ok {
		return
	}
	// delete domain
	err := d.DomainService.DeleteByID(
		g,
		session,
		id,
	)
	// handle response
	if ok := d.handleErrors(g, err); !ok {
		return
	}
	d.Response.OK(g, gin.H{})
}
