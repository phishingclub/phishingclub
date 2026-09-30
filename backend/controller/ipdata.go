package controller

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/ipdata"
	"github.com/phishingclub/phishingclub/service"
)

// IPData is a controller for the country and ASN data packages
type IPData struct {
	Common
	IPDataService *service.IPData
}

// Status returns the state of both data packages
func (c *IPData) Status(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	status, err := c.IPDataService.Status(g, session)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, gin.H{"packages": status})
}

// Download fetches and installs the package named in the path
func (c *IPData) Download(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	kind := g.Param("kind")
	err := c.IPDataService.Download(g, session, kind)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, nil)
}

// Remove deletes the installed package named in the path
func (c *IPData) Remove(g *gin.Context) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	kind := g.Param("kind")
	err := c.IPDataService.Remove(g, session, kind)
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, nil)
}

// SearchASN returns ASNs matching the query for the filter typeahead
func (c *IPData) SearchASN(g *gin.Context) {
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	q := g.Query("q")
	limit := 25
	if v := g.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	results := ipdata.Get().SearchASN(q, limit)
	c.Response.OK(g, gin.H{"results": results})
}

// LookupASN returns the autonomous systems that announce the given IP address
func (c *IPData) LookupASN(g *gin.Context) {
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	ip := g.Query("ip")
	if ip == "" {
		c.Response.BadRequest(g)
		return
	}
	store := ipdata.Get()
	results := store.LookupASNDetails(ip)
	c.Response.OK(g, gin.H{
		"ip":        ip,
		"available": store.ASNLoaded(),
		"found":     len(results) > 0,
		"results":   results,
	})
}

// ResolveASNRequest is the body for resolving configured ASNs to names.
type ResolveASNRequest struct {
	Asns []string `json:"asns"`
}

// ResolveASNs returns the details of the given ASNs that exist in the dataset.
// ASNs that are absent are left out, which lets the UI flag orphaned entries.
func (c *IPData) ResolveASNs(g *gin.Context) {
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	var req ResolveASNRequest
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	nums := make([]uint32, 0, len(req.Asns))
	for _, s := range req.Asns {
		if n, ok := parseASNInput(s); ok {
			nums = append(nums, n)
		}
	}
	results := ipdata.Get().ResolveASNs(nums)
	c.Response.OK(g, gin.H{"results": results})
}

// parseASNInput parses one ASN string with an optional AS or ASN prefix.
func parseASNInput(s string) (uint32, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.TrimPrefix(v, "asn")
	v = strings.TrimPrefix(v, "as")
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}
