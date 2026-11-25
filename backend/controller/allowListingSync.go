package controller

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"golang.org/x/time/rate"
)

// AllowListingSync is the controller of the allow listing sync. The admin
// handlers take the target scope from the optional companyID query, empty is
// the global target. The runner handlers are served by the phishing server.
type AllowListingSync struct {
	Common
	AllowListingSyncService *service.AllowListingSync
	// IPTrust is the phishing server proxy trust, the runner is served there
	IPTrust utils.IPTrust
	// RunnerLimiter limits authenticated runner calls per target
	RunnerLimiter KeyLimiter
}

// KeyLimiter hands out a rate limiter per key
type KeyLimiter interface {
	GetLimiter(key string) *rate.Limiter
}

// runner auth outcomes
const (
	runnerOK = iota
	runnerNotFound
	runnerLimited
)

// allowListingReportMaxBytes is the largest report body a runner may send
const allowListingReportMaxBytes = 256 << 10

// scope reads the session and the target scope
func (c *AllowListingSync) scope(g *gin.Context) (*model.Session, *uuid.UUID, bool) {
	session, _, ok := c.handleSession(g)
	if !ok {
		return nil, nil, false
	}
	companyID, ok := strictCompanyIDQuery(g, c.Response)
	if !ok {
		return nil, nil, false
	}
	return session, companyID, true
}

func (c *AllowListingSync) respond(g *gin.Context, result any, err error) {
	if ok := c.handleErrors(g, err); !ok {
		return
	}
	c.Response.OK(g, result)
}

// Get returns the target of a scope
func (c *AllowListingSync) Get(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	target, err := c.AllowListingSyncService.GetByCompanyID(g.Request.Context(), session, companyID)
	c.respond(g, target, err)
}

// Upsert creates the target of a scope or saves its settings
func (c *AllowListingSync) Upsert(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	var req model.AllowListingTargetSettings
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	target, err := c.AllowListingSyncService.Upsert(g.Request.Context(), session, companyID, &req)
	c.respond(g, target, err)
}

// Delete removes the target of a scope
func (c *AllowListingSync) Delete(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	// removeValues keeps the target until the runner has removed the values
	// Phishing Club added, it then deletes itself
	removeValues := g.Query("removeValues") == "true"
	target, err := c.AllowListingSyncService.DeleteByCompanyID(g.Request.Context(), session, companyID, removeValues)
	if target == nil {
		c.respond(g, gin.H{}, err)
		return
	}
	c.respond(g, target, err)
}

// RotateToken gives the target of a scope a new token
func (c *AllowListingSync) RotateToken(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	target, err := c.AllowListingSyncService.RotateToken(g.Request.Context(), session, companyID)
	c.respond(g, target, err)
}

// CancelDelete stops a delete that waits for the values to be removed
func (c *AllowListingSync) CancelDelete(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	target, err := c.AllowListingSyncService.CancelDelete(g.Request.Context(), session, companyID)
	c.respond(g, target, err)
}

// SetRunnerDomain stores the domain the runner of a scope calls
func (c *AllowListingSync) SetRunnerDomain(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	var req struct {
		Domain string `json:"domain"`
	}
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	target, err := c.AllowListingSyncService.SetRunnerDomain(g.Request.Context(), session, companyID, req.Domain)
	c.respond(g, target, err)
}

// Entries returns the ledger of the target of a scope
func (c *AllowListingSync) Entries(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	entries, err := c.AllowListingSyncService.Entries(g.Request.Context(), session, companyID)
	c.respond(g, entries, err)
}

// Preview returns what the next sync of the target of a scope would do
func (c *AllowListingSync) Preview(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	preview, err := c.AllowListingSyncService.Preview(g.Request.Context(), session, companyID)
	c.respond(g, preview, err)
}

// Runbook returns the runner script of the target of a scope, calling the
// domain in the domain query
func (c *AllowListingSync) Runbook(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	runbook, err := c.AllowListingSyncService.Runbook(g.Request.Context(), session, companyID, g.Query("domain"))
	c.respond(g, runbook, err)
}

// ListPins returns the pins of the target of a scope
func (c *AllowListingSync) ListPins(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	pins, err := c.AllowListingSyncService.ListPins(g.Request.Context(), session, companyID)
	c.respond(g, pins, err)
}

// CreatePin pins a template or value on the target of a scope
func (c *AllowListingSync) CreatePin(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	var req model.AllowListingPinInput
	if ok := c.handleParseRequest(g, &req); !ok {
		return
	}
	err := c.AllowListingSyncService.CreatePin(g.Request.Context(), session, companyID, &req)
	c.respond(g, gin.H{}, err)
}

// DeletePin removes a pin of the target of a scope
func (c *AllowListingSync) DeletePin(g *gin.Context) {
	session, companyID, ok := c.scope(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	err := c.AllowListingSyncService.DeletePin(g.Request.Context(), session, companyID, id)
	c.respond(g, gin.H{}, err)
}

// CampaignStatus returns the sync status of a campaign. The companyID query
// is the company in view.
func (c *AllowListingSync) CampaignStatus(g *gin.Context) {
	session, viewCompanyID, ok := c.scope(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	status, err := c.AllowListingSyncService.GetCampaignStatus(g.Request.Context(), session, id, viewCompanyID)
	c.respond(g, status, err)
}

// ProgramStatus returns the sync status of a program. The companyID query is
// the company in view.
func (c *AllowListingSync) ProgramStatus(g *gin.Context) {
	session, viewCompanyID, ok := c.scope(g)
	if !ok {
		return
	}
	id, ok := c.handleParseIDParam(g)
	if !ok {
		return
	}
	status, err := c.AllowListingSyncService.GetProgramStatus(g.Request.Context(), session, id, viewCompanyID)
	c.respond(g, status, err)
}

// runnerHostMatches reports if the host of a request is the runner domain
// of a target. A target without a runner domain is served on no host.
func runnerHostMatches(requestHost string, runnerDomain string) bool {
	if runnerDomain == "" {
		return false
	}
	host := requestHost
	if h, _, err := net.SplitHostPort(requestHost); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == runnerDomain
}

// runnerTarget authenticates a runner request. Not found means the caller
// must answer with the normal not found page, the same for every failure.
func (c *AllowListingSync) runnerTarget(g *gin.Context) (*model.AllowListingTarget, string, int) {
	ip := scimClientIP(g, c.IPTrust)
	auth := g.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, ip, runnerNotFound
	}
	// the token check is cheap and takes the same time for every target, so
	// there is no failure limit: a limit would only let a caller who knows
	// the path lock out a runner that shares its IP. Failed attempts are
	// audited with a throttle.
	target, ok := c.AllowListingSyncService.Authenticate(
		g.Request.Context(),
		g.Param("targetID"),
		strings.TrimSpace(strings.TrimPrefix(auth, "Bearer ")),
		ip,
	)
	// the runner of a target is only served on the domain saved for it, so a
	// token holder can not test which other hosts belong to this install
	if ok && !runnerHostMatches(g.Request.Host, target.RunnerDomain) {
		ok = false
	}
	if !ok {
		return nil, ip, runnerNotFound
	}
	if c.RunnerLimiter != nil && !c.RunnerLimiter.GetLimiter(target.ID.String()).Allow() {
		return nil, ip, runnerLimited
	}
	return target, ip, runnerOK
}

// RunnerPlan answers a runner with the next plan. It returns false when the
// request is not authenticated.
func (c *AllowListingSync) RunnerPlan(g *gin.Context) bool {
	target, _, outcome := c.runnerTarget(g)
	switch outcome {
	case runnerNotFound:
		return false
	case runnerLimited:
		g.AbortWithStatus(http.StatusTooManyRequests)
		return true
	}
	plan, err := c.AllowListingSyncService.Plan(g.Request.Context(), target)
	g.Header("Cache-Control", "no-store")
	if err != nil {
		g.JSON(http.StatusInternalServerError, gin.H{"error": "could not build the plan"})
		return true
	}
	g.JSON(http.StatusOK, plan)
	return true
}

// RunnerReport takes the lists a runner read back. It returns false when the
// request is not authenticated.
func (c *AllowListingSync) RunnerReport(g *gin.Context) bool {
	target, ip, outcome := c.runnerTarget(g)
	switch outcome {
	case runnerNotFound:
		return false
	case runnerLimited:
		g.AbortWithStatus(http.StatusTooManyRequests)
		return true
	}
	g.Header("Cache-Control", "no-store")
	g.Request.Body = http.MaxBytesReader(g.Writer, g.Request.Body, allowListingReportMaxBytes)
	var report model.AllowListingReport
	if err := json.NewDecoder(g.Request.Body).Decode(&report); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": "the report is not valid JSON or is too large"})
		return true
	}
	err := c.AllowListingSyncService.Report(g.Request.Context(), target, &report, ip)
	if err != nil {
		if errors.As(err, &errs.ValidationError{}) {
			g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return true
		}
		c.Logger.Errorw("failed to apply allow listing report", "error", err)
		g.JSON(http.StatusInternalServerError, gin.H{"error": "could not apply the report"})
		return true
	}
	g.JSON(http.StatusOK, gin.H{"ok": true})
	return true
}
