package controller

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/service"
)

// ReportTemplate is the report template controller
type ReportTemplate struct {
	Common
	ReportTemplateService *service.ReportTemplate
	CampaignService       *service.Campaign
	OptionService         *service.Option
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

// GetDefault returns the built in default report template content. The kind
// query parameter picks simulation, training or program. The older training
// parameter picks the training report.
func (r *ReportTemplate) GetDefault(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	kind := model.ReportKindSimulation
	if v := g.Query("training"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			r.Response.BadRequestMessage(g, "training must be true or false")
			return
		}
		if b {
			kind = model.ReportKindTraining
		}
	}
	if v := g.Query("kind"); v != "" {
		if !slices.Contains(model.ReportKinds, v) {
			r.Response.BadRequestMessage(g, "kind must be simulation, training or program")
			return
		}
		kind = v
	}
	content, err := r.ReportTemplateService.GetDefaultContent(session, kind)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.Response.OK(g, gin.H{"content": content})
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
	opt, _ := r.OptionService.GetOptionWithoutAuth(g.Request.Context(), data.OptionKeyReportPDFEnabled)
	if opt == nil || opt.Value.String() != "true" {
		r.Response.NotFound(g)
		return
	}
	id, ok := r.handleParseIDParam(g)
	if !ok {
		return
	}
	// the download is audited, the report names each recipient
	pdfBytes, campaignName, err := r.CampaignService.ExportReportPDF(
		g.Request.Context(),
		session,
		id,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	filename := fmt.Sprintf("report-%s.pdf", sanitizeFilename(campaignName))
	r.responseWithPDF(g, pdfBytes, filename)
}

// PreviewPDFRequest is the template content a PDF preview is rendered from.
// Kind is simulation, training or program; when empty IsTraining picks the
// training report.
type PreviewPDFRequest struct {
	Content    string `json:"content"`
	IsTraining bool   `json:"isTraining"`
	Kind       string `json:"kind"`
}

// PreviewPDF renders the posted report template with sample data to a PDF
func (r *ReportTemplate) PreviewPDF(g *gin.Context) {
	session, _, ok := r.handleSession(g)
	if !ok {
		return
	}
	opt, _ := r.OptionService.GetOptionWithoutAuth(g.Request.Context(), data.OptionKeyReportPDFEnabled)
	if opt == nil || opt.Value.String() != "true" {
		r.Response.NotFound(g)
		return
	}
	// a 1MB template, as stored templates allow, with room for JSON escaping
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, 2<<20)
	var req PreviewPDFRequest
	if ok := r.handleParseRequest(g, &req); !ok {
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = model.ReportKindSimulation
		if req.IsTraining {
			kind = model.ReportKindTraining
		}
	}
	if !slices.Contains(model.ReportKinds, kind) {
		r.Response.BadRequestMessage(g, "kind must be simulation, training or program")
		return
	}
	pdfBytes, err := r.ReportTemplateService.PreviewPDF(
		g.Request.Context(),
		session,
		req.Content,
		kind,
	)
	if ok := r.handleErrors(g, err); !ok {
		return
	}
	r.responseWithPDF(g, pdfBytes, "report-preview.pdf")
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

// sanitizeFilename removes characters that are unsafe in filenames, a name
// left empty is campaign
func sanitizeFilename(name string) string {
	return sanitizeFilenameOr(name, "campaign")
}

// sanitizeProgramFilename removes characters that are unsafe in filenames, a
// name left empty is program
func sanitizeProgramFilename(name string) string {
	return sanitizeFilenameOr(name, "program")
}

// sanitizeFilenameOr removes characters that are unsafe in filenames and
// returns fallback for a name left empty
func sanitizeFilenameOr(name, fallback string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-",
		"?", "-", "\"", "-", "<", "-", ">", "-", "|", "-",
	)
	safe := strings.TrimSpace(replacer.Replace(name))
	if safe == "" {
		return fallback
	}
	return safe
}
