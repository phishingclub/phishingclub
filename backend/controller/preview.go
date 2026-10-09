package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Preview renders admin authored preview content isolated from the admin origin.
type Preview struct {
	Common
}

// previewMaxBodyBytes caps the posted content so an authenticated request can
// not spill an unbounded body to disk during form parsing. A preview is a page
// or email template plus inline data urls, well under this.
const previewMaxBodyBytes = 20 << 20

// Render returns the posted content as an html document carrying a sandbox
// content security policy. The sandbox flag gives the document an opaque origin,
// so scripts in the previewed content can not read the admin session, cookies or
// call the API, while the document still gets its own policy and loads its own
// external images, styles, fonts and scripts for an accurate preview. The
// security headers middleware sets the strict admin policy on every response;
// this handler then replaces that header with its own sandbox policy, so the
// preview does not inherit it. allow-same-origin is deliberately absent.
func (p *Preview) Render(g *gin.Context) {
	_, _, ok := p.handleSession(g)
	if !ok {
		return
	}
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, previewMaxBodyBytes)
	content := g.PostForm("content")
	g.Header(
		"Content-Security-Policy",
		"sandbox allow-scripts allow-forms allow-popups allow-modals",
	)
	g.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
}
