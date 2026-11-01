package controller

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/service"
)

// ContentRegistry handlers only parse input; the service enforces the global
// permission and writes the audit log.
type ContentRegistry struct {
	Common
	ContentRegistryService *service.ContentRegistry
}

type addRegistryRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Token    string `json:"token"`
	Insecure bool   `json:"insecure"`
}

// AddRegistry connects a new registry
func (c *ContentRegistry) AddRegistry(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var req addRegistryRequest
	if !c.handleParseRequest(g, &req) {
		return
	}
	id, err := c.ContentRegistryService.AddRegistry(
		g.Request.Context(), session, req.Name, req.URL, req.Token, req.Insecure,
	)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"id": id.String()})
}

type updateRegistryRequest struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Token      string `json:"token"`
	ClearToken bool   `json:"clearToken"`
	Insecure   bool   `json:"insecure"`
}

// UpdateRegistry keeps the stored url and token when they are sent empty;
// clearToken removes the stored token.
func (c *ContentRegistry) UpdateRegistry(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req updateRegistryRequest
	if !c.handleParseRequest(g, &req) {
		return
	}
	err := c.ContentRegistryService.UpdateRegistry(
		g.Request.Context(), session, id, service.UpdateRegistryInput{
			Name:       req.Name,
			URL:        req.URL,
			Token:      req.Token,
			ClearToken: req.ClearToken,
			Insecure:   req.Insecure,
		},
	)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"ok": true})
}

// ListRegistries lists the configured registries
func (c *ContentRegistry) ListRegistries(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	rows, err := c.ContentRegistryService.ListRegistries(g.Request.Context(), session)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"registries": rows})
}

// RegistryStatus checks one registry. The list does not probe, so the UI asks
// each registry separately and shows the result as it arrives.
func (c *ContentRegistry) RegistryStatus(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	res, err := c.ContentRegistryService.RegistryStatus(g.Request.Context(), session, id)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"status": res})
}

// DeleteRegistry removes a registry connection
func (c *ContentRegistry) DeleteRegistry(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	err := c.ContentRegistryService.DeleteRegistry(g.Request.Context(), session, id)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"ok": true})
}

// Browse lists the packages of a registry
func (c *ContentRegistry) Browse(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	res, err := c.ContentRegistryService.Browse(g.Request.Context(), session, id)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"packages": res.Packages, "tagColors": res.TagColors})
}

// Inspect returns the verified contents of one package.
func (c *ContentRegistry) Inspect(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	namespace := g.Query("namespace")
	name := g.Query("name")
	if namespace == "" || name == "" {
		c.Response.BadRequestMessage(g, "namespace and name are required")
		return
	}
	res, err := c.ContentRegistryService.Inspect(g.Request.Context(), session, id, namespace, name)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"package": res})
}

// PreviewImage proxies a registry preview image so the browser loads it from
// the same origin.
func (c *ContentRegistry) PreviewImage(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	imgURL := g.Query("url")
	if imgURL == "" {
		c.Response.BadRequestMessage(g, "url is required")
		return
	}
	data, contentType, err := c.ContentRegistryService.PreviewImage(g.Request.Context(), session, id, imgURL)
	if !c.handleErrors(g, err) {
		return
	}
	// the bytes come from a remote registry. the service only passes a sniffed png,
	// jpeg, gif or webp, and if that ever slips these headers still stop the
	// browser from running scripts in the response.
	g.Header("Cache-Control", "private, max-age=300")
	g.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	g.Header("X-Content-Type-Options", "nosniff")
	g.Header("Content-Disposition", "inline")
	g.Data(200, contentType, data)
}

type installRequest struct {
	RegistryID string            `json:"registryId"`
	Namespace  string            `json:"namespace"`
	Name       string            `json:"name"`
	Variables  map[string]string `json:"variables"`
	// OverwriteReportTemplates lists the report template kinds the user confirmed
	// the package may overwrite.
	OverwriteReportTemplates []string `json:"overwriteReportTemplates"`
}

// reportTemplateOverwriteConflict answers 409 when a package would overwrite a
// global report template the user has not confirmed, listing each one so the UI
// can ask and retry. It reports whether it handled err.
func (c *ContentRegistry) reportTemplateOverwriteConflict(g *gin.Context, err error) bool {
	var ow service.ErrReportTemplateOverwrite
	if !errors.As(err, &ow) {
		return false
	}
	c.Response.Conflict(g, "Installing overwrites the current report template.", gin.H{"reportTemplates": ow.Templates})
	return true
}

// Install answers 409 for missing required variables and for a report template
// overwrite the user has not confirmed, so the UI can prompt and retry.
func (c *ContentRegistry) Install(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var req installRequest
	if !c.handleParseRequest(g, &req) {
		return
	}
	rid, err := uuid.Parse(req.RegistryID)
	if err != nil {
		c.Response.BadRequestMessage(g, "invalid registryId")
		return
	}
	registryID := &rid
	result, err := c.ContentRegistryService.Install(
		g.Request.Context(), session, registryID, req.Namespace, req.Name,
		req.Variables, req.OverwriteReportTemplates,
	)
	var missing service.ErrMissingVariables
	if errors.As(err, &missing) {
		c.Response.Conflict(g, "Some required values are missing.", gin.H{"missing": missing.Variables})
		return
	}
	if c.reportTemplateOverwriteConflict(g, err) {
		return
	}
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"result": result})
}

// InstalledList lists installed packages with update availability
func (c *ContentRegistry) InstalledList(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	rows, err := c.ContentRegistryService.ListInstalled(g.Request.Context(), session)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"installed": rows})
}

// InstalledResources lets the UI mark a resource as package managed wherever it
// is edited or listed.
func (c *ContentRegistry) InstalledResources(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	rows, err := c.ContentRegistryService.ListInstalledResources(g.Request.Context(), session)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"resources": rows})
}

type updateRequest struct {
	Variables map[string]string `json:"variables"`
	// Resolutions maps a conflicted resource ("type:sourceName") to the chosen
	// resolution: keep, overwrite or both. Unspecified conflicts default to keep.
	// A dropped referenced package ("member:packageId") takes keep or remove.
	Resolutions map[string]string `json:"resolutions"`
	// OverwriteReportTemplates lists the report template kinds the user confirmed
	// a report template new in this version may overwrite.
	OverwriteReportTemplates []string `json:"overwriteReportTemplates"`
}

// InstalledUpdate updates one installed package
func (c *ContentRegistry) InstalledUpdate(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	var req updateRequest
	if !c.handleParseOptionalRequest(g, &req) {
		return
	}
	result, err := c.ContentRegistryService.Update(
		g.Request.Context(), session, id, req.Variables, req.Resolutions, req.OverwriteReportTemplates,
	)
	var missing service.ErrMissingVariables
	if errors.As(err, &missing) {
		c.Response.Conflict(g, "Some required values are missing.", gin.H{"missing": missing.Variables})
		return
	}
	var conflicts service.ErrUpdateConflicts
	if errors.As(err, &conflicts) {
		c.Response.Conflict(g, "The update has items that need a choice.", gin.H{"conflicts": conflicts.Conflicts})
		return
	}
	if c.reportTemplateOverwriteConflict(g, err) {
		return
	}
	if errors.Is(err, service.ErrUninstalledDuringUpdate) {
		c.Response.Conflict(g, err.Error(), nil)
		return
	}
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"result": result})
}

// InstalledUninstallPreview reports what an uninstall would affect without
// changing anything.
func (c *ContentRegistry) InstalledUninstallPreview(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	res, err := c.ContentRegistryService.UninstallPreview(g.Request.Context(), session, id)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"preview": res})
}

// InstalledRemove removes one installed package
func (c *ContentRegistry) InstalledRemove(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	err := c.ContentRegistryService.Remove(g.Request.Context(), session, id)
	if !c.handleErrors(g, err) {
		return
	}
	c.Response.OK(g, gin.H{"ok": true})
}
