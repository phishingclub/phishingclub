package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/database"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// CampaignTemplateColumnsMap is a map between the frontend and the backend
// so the frontend has user friendly names instead of direct references
// to the database schema
// this is tied to a slice in the repository package
var CampaignTemplateColumnsMap = map[string]string{
	"created_at":                      repository.TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "created_at"),
	"updated_at":                      repository.TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "updated_at"),
	"name":                            repository.TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "name"),
	"after_landing_page_redirect_url": repository.TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "after_landing_page_redirect_url"),
	"is_complete":                     repository.TableColumn(database.CAMPAIGN_TEMPLATE_TABLE, "is_usable"),
	"domain":                          repository.TableColumn(database.DOMAIN_TABLE, "name"),
	"before_landing_page":             repository.TableColumn("before_landing_page", "name"),
	"landing_page":                    repository.TableColumn("landing_page", "name"),
	"after_landing_page":              repository.TableColumn("after_landing_page", "name"),
	"smtp":                            repository.TableColumn(database.SMTP_CONFIGURATION_TABLE, "name"),
	"api_sender":                      repository.TableColumn(database.API_SENDER_TABLE, "name"),
	"email":                           repository.TableColumn(database.EMAIL_TABLE, "name"),
}

// CampaignTemplate is a campaign template controller
type CampaignTemplate struct {
	Common
	CampaignTemplateService *service.CampaignTemplate
}

// Create creates a campaign template
func (c *CampaignTemplate) Create(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	var req model.CampaignTemplate
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// save
	ctx := g.Request.Context()
	id, err := c.CampaignTemplateService.Create(ctx, session, &req)
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

// GetByID gets a campaign template by id
func (c *CampaignTemplate) GetByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	// check if full data set should be loaded
	options := &repository.CampaignTemplateOption{}
	_, ok = g.GetQuery("full")
	if ok {
		options = &repository.CampaignTemplateOption{
			WithDomain:            true,
			WithSMTPConfiguration: true,
			WithAPISender:         true,
			WithEmail:             true,
			WithLandingPage:       true,
			WithBeforeLandingPage: true,
			WithAfterLandingPage:  true,
			WithIdentifier:        true,
		}
	}
	// get
	ctx := g.Request.Context()
	campaignTemplate, err := c.CampaignTemplateService.GetByID(
		ctx,
		session,
		id,
		options,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, campaignTemplate)
}

// GetAll gets all campaign templates
func (c *CampaignTemplate) GetAll(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	pagination, ok := c.handlePagination(g)
	if !ok {
		return
	}
	companyID := companyIDFromRequestQuery(g)
	queryArgs, ok := c.handleQueryArgs(g)
	if !ok {
		return
	}
	usableOnlyQuery := g.Query("usableOnly")
	usableOnly := false
	if usableOnlyQuery == "true" {
		usableOnly = true
	}
	queryArgs.DefaultSortByUpdatedAt()
	queryArgs.RemapOrderBy(CampaignTemplateColumnsMap)
	columns := repository.SelectTable(database.CAMPAIGN_TEMPLATE_TABLE)
	templates, err := c.CampaignTemplateService.GetAll(
		g,
		session,
		companyID,
		pagination,
		&repository.CampaignTemplateOption{
			QueryArgs:             queryArgs,
			Columns:               columns,
			WithDomain:            true,
			WithSMTPConfiguration: true,
			WithAPISender:         true,
			WithEmail:             true,
			WithLandingPage:       true,
			WithBeforeLandingPage: true,
			WithAfterLandingPage:  true,
			UsableOnly:            usableOnly,
		},
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, templates)
}

// UpdateByID updates a campaign template by id
func (c *CampaignTemplate) UpdateByID(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	// parse request
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.CampaignTemplate
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	// update
	err := c.CampaignTemplateService.UpdateByID(
		g.Request.Context(),
		session,
		id,
		&req,
	)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// DeleteByID deletes a campaign template by id
func (c *CampaignTemplate) DeleteByID(g *gin.Context) {
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
	err := c.CampaignTemplateService.DeleteByID(g, session, id)
	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}
