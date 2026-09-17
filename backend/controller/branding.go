package controller

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/service"
)

// Branding is the controller for install wide UI branding.
type Branding struct {
	Common
	BrandingService *service.Branding
}

// GetState returns the branding mode of each slot. It is public so the login
// screen can read it before authentication.
func (c *Branding) GetState(g *gin.Context) {
	state, err := c.BrandingService.GetState(g.Request.Context())
	if err != nil {
		c.Response.ServerError(g)
		return
	}
	// never cache the state so an admin change is picked up on the next load
	g.Header("Cache-Control", "no-store")
	c.Response.OK(g, state)
}

// GetImage streams the uploaded PNG for a slot. It is public so the login
// screen can show a custom logo and side image before authentication. When no
// custom image is stored it returns 404 so the frontend falls back to the
// built in default.
func (c *Branding) GetImage(g *gin.Context) {
	slot := g.Param("slot")
	content, found, err := c.BrandingService.GetImage(slot)
	if err != nil {
		c.Response.ServerError(g)
		return
	}
	if !found {
		c.Response.NotFound(g)
		return
	}
	g.Header("Cache-Control", "no-cache")
	g.Header("X-Content-Type-Options", "nosniff")
	g.Data(http.StatusOK, "image/png", content)
}

// Upload validates and stores an uploaded PNG for a slot.
func (c *Branding) Upload(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	slot := g.Param("slot")
	fileHeader, err := g.FormFile("file")
	if err != nil {
		c.Response.BadRequestMessage(g, "No file selected")
		return
	}
	// reject an oversized upload before reading it into memory
	if fileHeader.Size > data.BrandingMaxUploadBytes {
		c.Response.BadRequestMessage(g, "File is too large")
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		c.Response.BadRequest(g)
		return
	}
	defer f.Close()
	// cap the read so an oversized upload can not exhaust memory, one byte over
	// the limit so the size validation still rejects it
	content, err := io.ReadAll(io.LimitReader(f, data.BrandingMaxUploadBytes+1))
	if err != nil {
		c.Response.BadRequest(g)
		return
	}
	err = c.BrandingService.SetImage(g.Request.Context(), session, slot, content)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// Reset removes the uploaded image for a slot, returning it to the default.
func (c *Branding) Reset(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	slot := g.Param("slot")
	err := c.BrandingService.Reset(g.Request.Context(), session, slot)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// SetDisplay stores the display fit settings for a slot.
func (c *Branding) SetDisplay(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	slot := g.Param("slot")
	var req service.BrandingDisplay
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	err := c.BrandingService.SetDisplay(g.Request.Context(), session, slot, req)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}

// SetSideImageVisibility shows or hides the login side image. Hiding centers
// the login form.
func (c *Branding) SetSideImageVisibility(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var req struct {
		Hidden bool `json:"hidden"`
	}
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	err := c.BrandingService.SetSideImageHidden(g.Request.Context(), session, req.Hidden)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{})
}
