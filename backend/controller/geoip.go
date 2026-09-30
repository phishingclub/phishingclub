package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/ipdata"
)

// GeoIP is a controller for GeoIP related endpoints
type GeoIP struct {
	Common
}

// GetMetadata returns the available country codes for the filter UI
func (c *GeoIP) GetMetadata(g *gin.Context) {
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}

	codes := ipdata.Get().CountryCodes()

	c.Response.OK(g, gin.H{
		"country_codes": codes,
		"countries":     ipdata.Get().Countries(),
		"asn_available": ipdata.Get().ASNLoaded(),
	})
}

// Lookup performs a country lookup for the provided IP address
func (c *GeoIP) Lookup(g *gin.Context) {
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}

	ip := g.Query("ip")
	if ip == "" {
		c.Response.BadRequest(g)
		return
	}

	countryCode, found := ipdata.Get().LookupCountry(ip)

	result := gin.H{
		"ip":    ip,
		"found": found,
	}
	if found {
		result["country_code"] = countryCode
	}

	c.Response.OK(g, result)
}
