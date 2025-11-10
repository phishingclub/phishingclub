package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/geoip"
)

// GeoIP is a controller for GeoIP-related endpoints
type GeoIP struct {
	Common
}

// GetMetadata returns the GeoIP metadata including available country codes
func (c *GeoIP) GetMetadata(g *gin.Context) {
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}

	// get geoip instance
	geo, err := geoip.Instance()
	if ok := c.handleErrors(g, err); !ok {
		return
	}

	// get metadata
	metadata := geo.GetMetadata()
	if metadata == nil {
		c.Response.BadRequest(g)
		return
	}

	c.Response.OK(g, metadata)
}

// Lookup performs a GeoIP lookup for the provided IP address
func (c *GeoIP) Lookup(g *gin.Context) {
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}

	// get ip from query parameter
	ip := g.Query("ip")
	if ip == "" {
		c.Response.BadRequest(g)
		return
	}

	// get geoip instance
	geo, err := geoip.Instance()
	if ok := c.handleErrors(g, err); !ok {
		return
	}

	// perform lookup
	countryCode, found := geo.Lookup(ip)

	// return result
	result := gin.H{
		"ip":    ip,
		"found": found,
	}

	if found {
		result["country_code"] = countryCode
	}

	c.Response.OK(g, result)
}
