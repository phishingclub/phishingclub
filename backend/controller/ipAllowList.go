package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/service"
)

// IPAllowList is the controller for IP allow list management
type IPAllowList struct {
	Common
	IPAllowListService *service.IPAllowListService
}

// GetEntriesForProxyConfig returns IP allow list entries for a specific proxy configuration
func (c *IPAllowList) GetEntriesForProxyConfig(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}

	// parse proxy config ID from URL params
	proxyConfigID, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}

	// get entries for proxy config
	entries, err := c.IPAllowListService.GetEntriesForProxyConfig(
		g.Request.Context(),
		session,
		proxyConfigID,
	)

	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, entries)
}

// ClearForProxyConfig removes all entries for a specific proxy configuration
func (c *IPAllowList) ClearForProxyConfig(g *gin.Context) {
	// handle session
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}

	// parse proxy config ID from URL params
	proxyConfigID, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}

	// clear entries for proxy config
	count, err := c.IPAllowListService.ClearForProxyConfig(
		g.Request.Context(),
		session,
		proxyConfigID,
	)

	// handle response
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, map[string]interface{}{
		"message":       "Entries cleared for proxy configuration",
		"cleared_count": count,
	})
}
