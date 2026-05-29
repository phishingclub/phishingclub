package controller

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/remotebrowser"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// ReportTemplate is the report template controller
type ReportTemplate struct {
	Common
	ReportTemplateService *service.ReportTemplate
	CampaignService       *service.Campaign
	ExecPath              string
}

// Create creates a report template
func (r *ReportTemplate) Create(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	var req model.ReportTemplate
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	id, err := r.ReportTemplateService.Create(g.Request.Context(), session, &req)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, gin.H{"id": id.String()})
}

// GetAll gets report templates
func (r *ReportTemplate) GetAll(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	queryArgs, ok := r.handleQueryArgs(g)
	if !ok {
		return
	}
	queryArgs.DefaultSortByUpdatedAt()
	companyID := companyIDFromRequestQuery(g)
	result, err := r.ReportTemplateService.GetAll(
		g.Request.Context(),
		session,
		companyID,
		&repository.ReportTemplateOption{QueryArgs: queryArgs},
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, result)
}

// GetByID gets a report template by id
func (r *ReportTemplate) GetByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	tmpl, err := r.ReportTemplateService.GetByID(g.Request.Context(), session, id)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, tmpl)
}

// UpdateByID updates a report template
func (r *ReportTemplate) UpdateByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	var req model.ReportTemplate
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	err := r.ReportTemplateService.UpdateByID(g.Request.Context(), session, id, &req)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, gin.H{})
}

// DeleteByID deletes a report template
func (r *ReportTemplate) DeleteByID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	err := r.ReportTemplateService.DeleteByID(g.Request.Context(), session, id)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, gin.H{})
}

// GeneratePDFByCampaignID generates a PDF report for a campaign
func (r *ReportTemplate) GeneratePDFByCampaignID(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	htmlContent, campaignName, err := r.CampaignService.BuildReportHTML(
		g.Request.Context(),
		session,
		id,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	pdfBytes, err := remotebrowser.RenderHTMLToPDF(g.Request.Context(), htmlContent, r.ExecPath)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	filename := fmt.Sprintf("report-%s.pdf", sanitizeFilename(campaignName))
	r.responseWithPDF(g, pdfBytes, filename)
}

// WipeBrowserCache deletes the auto-downloaded Chromium binary used for PDF generation.
func (r *ReportTemplate) WipeBrowserCache(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	if err := r.ReportTemplateService.WipeBrowserCache(session); err != nil {
		r.handleErrors(g, err)
		return
	}
	r.Response.OK(g, gin.H{})
}

// sanitizeFilename removes characters that are unsafe in filenames
func sanitizeFilename(name string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-",
		"?", "-", "\"", "-", "<", "-", ">", "-", "|", "-",
	)
	safe := strings.TrimSpace(replacer.Replace(name))
	if safe == "" {
		return "campaign"
	}
	return safe
}
