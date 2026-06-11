package controller

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/service"
	"gorm.io/gorm"
)

const (
	scimContentType = "application/scim+json"
)

// Scim is the SCIM v2 protocol controller.
// all handlers authenticate via bearer token before dispatching to the service layer.
type Scim struct {
	Common
	ScimService *service.Scim
}

// scimError writes a SCIM-compliant error response
func scimError(g *gin.Context, status int, detail string, scimType ...string) {
	body := service.ScimError{
		Schemas: []string{"urn:ietf:params:scim:api:messages:2.0:Error"},
		Status:  status,
		Detail:  detail,
	}
	if len(scimType) > 0 {
		body.ScimType = scimType[0]
	}
	g.Header("Content-Type", scimContentType)
	g.JSON(status, body)
	g.Abort()
}

// scimOK writes a SCIM-compliant success response with Content-Type application/scim+json
func scimOK(g *gin.Context, status int, body any) {
	g.Header("Content-Type", scimContentType)
	g.JSON(status, body)
}

// authenticate validates the Authorization: Bearer <token> header and returns
// the verified SCIM config for the company. on failure it writes the error
// response itself and returns nil, false.
func (c *Scim) authenticate(g *gin.Context, companyID *uuid.UUID) (*service.ScimConfigResult, bool) {
	authHeader := g.GetHeader("Authorization")
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		scimError(g, http.StatusUnauthorized, "missing or malformed Authorization header")
		return nil, false
	}
	token := strings.TrimPrefix(authHeader, bearerPrefix)
	if token == "" {
		scimError(g, http.StatusUnauthorized, "empty bearer token")
		return nil, false
	}

	config, authed, err := c.ScimService.VerifyAndLoadConfig(g.Request.Context(), companyID, token)
	if err != nil {
		// a missing config is reported with the same generic 401 as a bad token so
		// the response does not reveal whether a company has SCIM configured
		if isNotFound(err) {
			scimError(g, http.StatusUnauthorized, "invalid bearer token or SCIM provisioning is disabled")
			return nil, false
		}
		c.Logger.Errorw("scim auth: error verifying token", "error", err)
		scimError(g, http.StatusInternalServerError, "internal error during authentication")
		return nil, false
	}
	if !authed {
		scimError(g, http.StatusUnauthorized, "invalid bearer token or SCIM provisioning is disabled")
		return nil, false
	}
	return &service.ScimConfigResult{Config: config, CompanyID: companyID}, true
}

// parseCompanyID extracts and parses the :companyID path parameter.
// writes the error response and returns false on failure.
func (c *Scim) parseCompanyID(g *gin.Context) (*uuid.UUID, bool) {
	id, err := uuid.Parse(g.Param("companyID"))
	if err != nil {
		scimError(g, http.StatusBadRequest, "invalid companyID in URL path")
		return nil, false
	}
	return &id, true
}

// parseUserID extracts and parses the :userID path parameter.
// an unparseable id means the resource cannot exist, so 404 is correct per rfc 7644.
func (c *Scim) parseUserID(g *gin.Context) (*uuid.UUID, bool) {
	id, err := uuid.Parse(g.Param("userID"))
	if err != nil {
		scimError(g, http.StatusNotFound, "user not found")
		return nil, false
	}
	return &id, true
}

// parseGroupID extracts and parses the :groupID path parameter.
// an unparseable id means the resource cannot exist, so 404 is correct per rfc 7644.
func (c *Scim) parseGroupID(g *gin.Context) (*uuid.UUID, bool) {
	id, err := uuid.Parse(g.Param("groupID"))
	if err != nil {
		scimError(g, http.StatusNotFound, "group not found")
		return nil, false
	}
	return &id, true
}

// scimBaseURL builds the base URL for SCIM resource locations from the request
func scimBaseURL(g *gin.Context, companyID *uuid.UUID) string {
	scheme := "https"
	if g.Request.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + g.Request.Host + "/api/v1/scim/v2/" + companyID.String()
}

// isNotFound returns true when the error wraps gorm.ErrRecordNotFound
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), gorm.ErrRecordNotFound.Error())
}

// ── ServiceProviderConfig ─────────────────────────────────────────────────────

// ServiceProviderConfig handles GET /scim/v2/:companyID/ServiceProviderConfig
func (c *Scim) ServiceProviderConfig(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	if _, ok := c.authenticate(g, companyID); !ok {
		return
	}
	base := scimBaseURL(g, companyID)
	scimOK(g, http.StatusOK, c.ScimService.ServiceProviderConfig(base))
}

// ── Schemas ───────────────────────────────────────────────────────────────────

// Schemas handles GET /api/v1/scim/v2/:companyID/Schemas
func (c *Scim) Schemas(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	if _, ok := c.authenticate(g, companyID); !ok {
		return
	}
	base := scimBaseURL(g, companyID)
	schemas := c.ScimService.Schemas(base)
	scimOK(g, http.StatusOK, gin.H{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(schemas),
		"startIndex":   1,
		"itemsPerPage": len(schemas),
		"Resources":    schemas,
	})
}

// GetSchema handles GET /api/v1/scim/v2/:companyID/Schemas/:schemaID
func (c *Scim) GetSchema(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	if _, ok := c.authenticate(g, companyID); !ok {
		return
	}
	schemaID := g.Param("schemaID")
	base := scimBaseURL(g, companyID)
	schema, found := c.ScimService.GetSchemaByID(base, schemaID)
	if !found {
		scimError(g, http.StatusNotFound, "schema not found")
		return
	}
	scimOK(g, http.StatusOK, schema)
}

// ── ResourceTypes ─────────────────────────────────────────────────────────────

// ResourceTypes handles GET /scim/v2/:companyID/ResourceTypes
func (c *Scim) ResourceTypes(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	if _, ok := c.authenticate(g, companyID); !ok {
		return
	}
	base := scimBaseURL(g, companyID)
	types := c.ScimService.ResourceTypes(base)
	scimOK(g, http.StatusOK, gin.H{
		"schemas":      []string{"urn:ietf:params:scim:api:messages:2.0:ListResponse"},
		"totalResults": len(types),
		"startIndex":   1,
		"itemsPerPage": len(types),
		"Resources":    types,
	})
}

// GetResourceType handles GET /scim/v2/:companyID/ResourceTypes/:resourceTypeID
func (c *Scim) GetResourceType(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	if _, ok := c.authenticate(g, companyID); !ok {
		return
	}
	resourceTypeID := g.Param("resourceTypeID")
	base := scimBaseURL(g, companyID)
	rt, found := c.ScimService.GetResourceTypeByID(base, resourceTypeID)
	if !found {
		scimError(g, http.StatusNotFound, "resource type not found")
		return
	}
	scimOK(g, http.StatusOK, rt)
}

// ── Groups ────────────────────────────────────────────────────────────────────

// ListGroups handles GET /api/v1/scim/v2/:companyID/Groups
func (c *Scim) ListGroups(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	base := scimBaseURL(g, companyID)
	startIndex := parseIntQuery(g, "startIndex", 1)
	count := parseIntQuery(g, "count", -1)
	filter := g.Query("filter")
	excludedAttributes := g.Query("excludedAttributes")

	list, err := c.ScimService.ListGroupsRaw(g.Request.Context(), companyID, result.Config, base, startIndex, count, filter, excludedAttributes)
	if err != nil {
		c.Logger.Errorw("scim list groups: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to list groups")
		return
	}
	scimOK(g, http.StatusOK, list)
}

// GetGroup handles GET /api/v1/scim/v2/:companyID/Groups/:groupID
func (c *Scim) GetGroup(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	groupID, ok := c.parseGroupID(g)
	if !ok {
		return
	}
	base := scimBaseURL(g, companyID)

	group, err := c.ScimService.GetGroup(g.Request.Context(), companyID, result.Config, groupID, base)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "group not found")
			return
		}
		c.Logger.Errorw("scim get group: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to get group")
		return
	}
	scimOK(g, http.StatusOK, group)
}

// CreateGroup handles POST /api/v1/scim/v2/:companyID/Groups
func (c *Scim) CreateGroup(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}

	var req service.ScimGroup
	if err := g.ShouldBindJSON(&req); err != nil {
		scimError(g, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalidSyntax")
		return
	}

	base := scimBaseURL(g, companyID)
	created, err := c.ScimService.CreateGroup(g.Request.Context(), companyID, result.Config, &req, base)
	if err != nil {
		if isSyntaxError(err) {
			scimError(g, http.StatusBadRequest, err.Error(), "invalidSyntax")
			return
		}
		if isConflictError(err) {
			scimError(g, http.StatusConflict, err.Error(), "uniqueness")
			return
		}
		if isValidationError(err) {
			scimError(g, http.StatusBadRequest, err.Error(), "invalidValue")
			return
		}
		c.Logger.Errorw("scim create group: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to create group")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	scimOK(g, http.StatusCreated, created)
}

// ReplaceGroup handles PUT /api/v1/scim/v2/:companyID/Groups/:groupID
func (c *Scim) ReplaceGroup(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	groupID, ok := c.parseGroupID(g)
	if !ok {
		return
	}

	var req service.ScimGroup
	if err := g.ShouldBindJSON(&req); err != nil {
		scimError(g, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalidValue")
		return
	}

	base := scimBaseURL(g, companyID)
	updated, err := c.ScimService.ReplaceGroup(g.Request.Context(), companyID, result.Config, groupID, &req, base)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "group not found")
			return
		}
		if isValidationError(err) {
			scimError(g, http.StatusBadRequest, err.Error(), "invalidValue")
			return
		}
		c.Logger.Errorw("scim replace group: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to replace group")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	scimOK(g, http.StatusOK, updated)
}

// PatchGroup handles PATCH /api/v1/scim/v2/:companyID/Groups/:groupID
func (c *Scim) PatchGroup(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	groupID, ok := c.parseGroupID(g)
	if !ok {
		return
	}

	var req service.ScimGroupPatchOp
	if err := g.ShouldBindJSON(&req); err != nil {
		scimError(g, http.StatusBadRequest, "invalid patch body: "+err.Error(), "invalidValue")
		return
	}

	base := scimBaseURL(g, companyID)
	updated, err := c.ScimService.PatchGroup(g.Request.Context(), companyID, result.Config, groupID, &req, base)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "group not found")
			return
		}
		if isValidationError(err) {
			scimError(g, http.StatusBadRequest, err.Error(), "invalidValue")
			return
		}
		c.Logger.Errorw("scim patch group: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to patch group")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	scimOK(g, http.StatusOK, updated)
}

// DeleteGroup handles DELETE /api/v1/scim/v2/:companyID/Groups/:groupID
func (c *Scim) DeleteGroup(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	groupID, ok := c.parseGroupID(g)
	if !ok {
		return
	}

	err := c.ScimService.DeleteGroup(g.Request.Context(), companyID, result.Config, groupID)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "group not found")
			return
		}
		c.Logger.Errorw("scim delete group: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to delete group")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	// rfc 7644 §3.6 — successful DELETE returns 204 No Content
	g.Status(http.StatusNoContent)
}

// ── Users ─────────────────────────────────────────────────────────────────────

// ListUsers handles GET /scim/v2/:companyID/Users
func (c *Scim) ListUsers(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	base := scimBaseURL(g, companyID)
	filter := g.Query("filter")
	startIndex := parseIntQuery(g, "startIndex", 1)
	count := parseIntQuery(g, "count", -1)
	sortBy := g.Query("sortBy")
	sortOrder := g.Query("sortOrder")

	list, err := c.ScimService.ListUsers(g.Request.Context(), companyID, result.Config, base, filter, startIndex, count, sortBy, sortOrder)
	if err != nil {
		c.Logger.Errorw("scim list users: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to list users")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	scimOK(g, http.StatusOK, list)
}

// GetUser handles GET /scim/v2/:companyID/Users/:userID
func (c *Scim) GetUser(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	userID, ok := c.parseUserID(g)
	if !ok {
		return
	}
	base := scimBaseURL(g, companyID)

	user, err := c.ScimService.GetUser(g.Request.Context(), companyID, result.Config, userID, base)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "user not found")
			return
		}
		c.Logger.Errorw("scim get user: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to get user")
		return
	}
	scimOK(g, http.StatusOK, user)
}

// CreateUser handles POST /scim/v2/:companyID/Users
func (c *Scim) CreateUser(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}

	var req service.ScimUser
	if err := g.ShouldBindJSON(&req); err != nil {
		scimError(g, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalidValue")
		return
	}

	base := scimBaseURL(g, companyID)
	created, err := c.ScimService.CreateUser(g.Request.Context(), companyID, result.Config, &req, base)
	if err != nil {
		if isConflictError(err) {
			scimError(g, http.StatusConflict, err.Error(), "uniqueness")
			return
		}
		if isValidationError(err) {
			scimError(g, http.StatusBadRequest, err.Error(), "invalidValue")
			return
		}
		c.Logger.Errorw("scim create user: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to create user")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	scimOK(g, http.StatusCreated, created)
}

// ReplaceUser handles PUT /scim/v2/:companyID/Users/:userID
func (c *Scim) ReplaceUser(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	userID, ok := c.parseUserID(g)
	if !ok {
		return
	}

	var req service.ScimUser
	if err := g.ShouldBindJSON(&req); err != nil {
		scimError(g, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalidValue")
		return
	}

	base := scimBaseURL(g, companyID)
	updated, err := c.ScimService.ReplaceUser(g.Request.Context(), companyID, result.Config, userID, &req, base)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "user not found")
			return
		}
		if isValidationError(err) {
			scimError(g, http.StatusBadRequest, err.Error(), "invalidValue")
			return
		}
		c.Logger.Errorw("scim replace user: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to replace user")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	scimOK(g, http.StatusOK, updated)
}

// PatchUser handles PATCH /scim/v2/:companyID/Users/:userID
func (c *Scim) PatchUser(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	userID, ok := c.parseUserID(g)
	if !ok {
		return
	}

	var req service.ScimPatchOp
	if err := g.ShouldBindJSON(&req); err != nil {
		scimError(g, http.StatusBadRequest, "invalid patch body: "+err.Error(), "invalidValue")
		return
	}

	base := scimBaseURL(g, companyID)
	updated, err := c.ScimService.PatchUser(g.Request.Context(), companyID, result.Config, userID, &req, base)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "user not found")
			return
		}
		if isValidationError(err) {
			scimError(g, http.StatusBadRequest, err.Error(), "invalidValue")
			return
		}
		c.Logger.Errorw("scim patch user: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to patch user")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	scimOK(g, http.StatusOK, updated)
}

// DeleteUser handles DELETE /scim/v2/:companyID/Users/:userID
func (c *Scim) DeleteUser(g *gin.Context) {
	companyID, ok := c.parseCompanyID(g)
	if !ok {
		return
	}
	result, ok := c.authenticate(g, companyID)
	if !ok {
		return
	}
	userID, ok := c.parseUserID(g)
	if !ok {
		return
	}

	err := c.ScimService.DeprovisionUser(g.Request.Context(), companyID, result.Config, userID)
	if err != nil {
		if isNotFound(err) {
			scimError(g, http.StatusNotFound, "user not found")
			return
		}
		c.Logger.Errorw("scim delete user: error", "error", err)
		scimError(g, http.StatusInternalServerError, "failed to deprovision user")
		return
	}
	go c.ScimService.UpdateLastSync(context.Background(), result.Config)
	// RFC 7644 §3.6 — successful DELETE returns 204 No Content
	g.Status(http.StatusNoContent)
}

// isValidationError returns true when the error wraps an errs.ValidationError
func isValidationError(err error) bool {
	if err == nil {
		return false
	}
	var target errs.ValidationError
	return errors.As(err, &target)
}

// isSyntaxError returns true when the error wraps an errs.SyntaxError
func isSyntaxError(err error) bool {
	if err == nil {
		return false
	}
	var target errs.SyntaxError
	return errors.As(err, &target)
}

// isConflictError returns true when the error wraps an errs.ConflictError
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	var target errs.ConflictError
	return errors.As(err, &target)
}

// parseIntQuery parses a query param as an int, returning defaultVal if absent or invalid.
func parseIntQuery(g *gin.Context, key string, defaultVal int) int {
	raw := g.Query(key)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return defaultVal
	}
	return v
}
