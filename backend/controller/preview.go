package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Preview renders preview content isolated from the admin origin: content an
// admin authored and content from third party packages.
type Preview struct {
	Common
}

// previewMaxBodyBytes caps the posted content so an authenticated request can
// not spill an unbounded body to disk during form parsing. A preview is a page
// or email template plus inline data urls, well under this.
const previewMaxBodyBytes = 20 << 20

// Render returns the posted content as an html document carrying a sandbox
// content security policy. The sandbox flag gives the document an opaque origin,
// so scripts in the previewed content cannot read the admin session, cookies or
// call the API, while the document still gets its own policy and loads its own
// external images, styles, fonts and scripts for an accurate preview. The
// security headers middleware sets the strict admin policy on every response;
// this handler then replaces that header with its own sandbox policy, so the
// preview does not inherit it. allow-same-origin is deliberately absent.
func (p *Preview) Render(g *gin.Context) {
	p.render(g, adminPreviewCSP, "")
}

// RenderPackage returns posted Bait Box package html under packagePreviewCSP.
// Package content is third party, so it gets a tighter policy than the admin's
// own templates.
func (p *Preview) RenderPackage(g *gin.Context) {
	p.render(g, packagePreviewCSP, packagePreviewReferrer)
}

// packagePreviewReferrer stops remote images in package html from learning the
// admin host.
const packagePreviewReferrer = "no-referrer"

// adminPreviewCSP covers the admin's own templates.
const adminPreviewCSP = "sandbox allow-scripts allow-forms allow-popups allow-modals"

// packagePreviewCSP runs package html in an opaque origin sandbox. Images, fonts,
// styles and media may load from any https host so the page looks as sent.
// Scripts run only from inline code and data or blob urls. fetch, xhr, form
// posts, plugins and base changes are blocked, there are no modal dialogs, and
// only the admin app may frame it. allow-forms stays so a page's own submit
// handlers run; form-action 'none' still blocks the request.
const packagePreviewCSP = "sandbox allow-scripts allow-forms allow-popups; " +
	"default-src data: blob:; " +
	"script-src 'unsafe-inline' 'unsafe-eval' data: blob:; " +
	"style-src 'unsafe-inline' data: https:; " +
	"img-src data: blob: https:; " +
	"font-src data: https:; " +
	"media-src data: blob: https:; " +
	"connect-src 'none'; " +
	"form-action 'none'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'self'"

func (p *Preview) render(g *gin.Context, csp, referrerPolicy string) {
	session, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	if !p.handleGlobalAuthorization(g, session) {
		return
	}
	writePreview(g, csp, referrerPolicy)
}

// writePreview replaces the policy headers set by the security headers
// middleware and writes the posted content. An empty referrerPolicy keeps the
// middleware value.
func writePreview(g *gin.Context, csp, referrerPolicy string) {
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, previewMaxBodyBytes)
	content := g.PostForm("content")
	g.Header("Content-Security-Policy", csp)
	if referrerPolicy != "" {
		g.Header("Referrer-Policy", referrerPolicy)
	}
	g.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
}
