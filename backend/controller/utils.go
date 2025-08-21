package controller

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
	"github.com/phishingclub/phishingclub/utils"
	"github.com/phishingclub/phishingclub/vo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Common is a common controller base struct it holds common operations on the
// common dependencies
type Common struct {
	Response       api.JSONResponseHandler
	Logger         *zap.SugaredLogger
	SessionService *service.Session
}

// handleSession handles the session and returns the session and user
// if the session is not valid, a 401 response is sent
func (c *Common) handleSession(
	g *gin.Context,
) (*model.Session, *model.User, bool) {
	s, ok := g.Get("session")
	if !ok {
		c.Logger.Debug("session not found in context")
		c.Response.Unauthorized(g)
		return nil, nil, false
	}
	session, ok := s.(*model.Session)
	if !ok {
		c.Logger.Error("session in context is not of type model.Session")
		c.Response.Unauthorized(g)
		return nil, nil, false
	}
	user := session.User
	if user == nil {
		c.Logger.Error("user not found in session")
		c.Response.Unauthorized(g)
		return nil, nil, false
	}
	return session, user, true
}

// HandleParseRequest parses the request and returns true if successful
// if the request is not parsable, a 400 response is sent
func (c *Common) handleParseRequest(
	g *gin.Context,
	req any,
) bool {
	body, err := io.ReadAll(g.Request.Body)
	if err != nil {
		c.Logger.Debugw("failed to read request body",
			"error", err,
		)
		c.Response.BadRequest(g)
		return false
	}
	if err := utils.Unmarshal(body, &req); err != nil {
		c.Logger.Debugw("failed to parse request",
			"error", err,
		)
		c.Response.BadRequestMessage(g, err.Error())
		return false
	}
	return true
}

// handleParseIDParam parses the id parameter from the request
// and returns it if successful
// if the id is not parsable, a 400 response is sent
func (c *Common) handleParseIDParam(
	g *gin.Context,
) (*uuid.UUID, bool) {
	id, err := uuid.Parse(g.Param("id"))
	if err != nil {
		c.Logger.Debugw("failed to parse id",
			"error", err,
		)
		c.Response.BadRequestMessage(g, errs.MsgFailedToParseUUID)
		return nil, false
	}
	return &id, true
}

// handlePagination parses the pagination from the request and returns it
// if the pagination is not valid, a 400 response is sent
func (c *Common) handlePagination(
	g *gin.Context,
) (*vo.Pagination, bool) {
	pagination, err := vo.NewPaginationFromRequest(g)
	if err != nil {
		c.Logger.Debugw("invalid offset or limit",
			"error", err,
		)
		c.Response.ValidationFailed(g, "pagination", err)
		return nil, false
	}
	return pagination, true
}

// handleQueryArgs parses the query from the request and returns it
func (c *Common) handleQueryArgs(g *gin.Context) (*vo.QueryArgs, bool) {
	q, err := vo.QueryFromRequest(g)
	if err != nil {
		c.Logger.Debugw("failed to parse query",
			"error", err,
		)
		c.Response.ValidationFailed(g, "query args", err)
		return nil, false
	}
	return q, true
}

// handleErrors is a helper function to handle common handleErrors
// it most often checks for more than what is needed, but is
// useful to avoid missing any error handling and saving time
// it returns true if no errors are found
// it returns false if an error is found and a response is sent
func (c *Common) handleErrors(
	g *gin.Context,
	err error,
) bool {
	if err != nil {
		if ok := handleAuthorizationError(g, c.Response, err); !ok {
			c.Logger.Debugw("authorization error",
				"auth_error", err,
			)
			return false
		}
		if ok := handleValidationError(g, c.Response, err); !ok {
			c.Logger.Debugw("validation error",
				"validation_error", err,
			)
			return false
		}
		if ok := handleCustomError(g, c.Response, err); !ok {
			c.Logger.Debugw("custom error",
				"custom_error", err,
			)
			return false
		}
		if ok := handleDBRowNotFound(g, c.Response, err); !ok {
			c.Logger.Debugw("DB row not found error",
				"error", err,
			)
			return false
		}
		c.Logger.Errorw("API unknown error type", "error", err)
		_ = handleServerError(g, c.Response, err)
		return false
	}
	return true
}

// responseWithCSV
func (c *Common) responseWithCSV(
	g *gin.Context,
	buffer *bytes.Buffer,
	writer *csv.Writer,
	name string,
) {
	writer.Flush()
	if err := writer.Error(); err != nil {
		c.handleErrors(g, err)
		return
	}
	// Set CSV response headers
	setSecureContentDisposition(g, name)
	g.Header("Content-Type", "text/csv")
	g.Header("Content-Length", fmt.Sprint(buffer.Len()))

	// Write the CSV buffer to the response
	_, err := g.Writer.Write(buffer.Bytes())
	if err != nil {
		c.handleErrors(g, err)
	}
}

// responseWithZIP
func (c *Common) responseWithZIP(
	g *gin.Context,
	buffer *bytes.Buffer,
	name string,
) {
	g.Header("Content-Type", "application/zip")
	setSecureContentDisposition(g, name)
	g.Header("Content-Transfer-Encoding", "binary")
	g.Header("Expires", "0")
	g.Header("Cache-Control", "must-revalidate")
	g.Header("Pragma", "public")
	g.Header("Content-Length", fmt.Sprintf("%d", buffer.Len()))

	_, err := g.Writer.Write(buffer.Bytes())
	if err != nil {
		c.handleErrors(g, err)
	}
}

// companyIDFromRequestQuery returns the companyID as a UUID from the query
// or nil if not found
func companyIDFromRequestQuery(g *gin.Context) *uuid.UUID {
	companyID := g.Query("companyID")
	if companyID != "" {
		cid, err := uuid.Parse(companyID)
		if err != nil {
			return nil
		}
		return &cid
	}
	return nil
}

// SetSessionInGinContext sets the session in the gin context
func SetSessionInGinContext(c *gin.Context, s *model.Session) {
	c.Set("session", s)
}

// handleDBRowNotFound checks if the error is a not found error
// if it is, a 404 response is sent
// if it is not, true is returned
func handleDBRowNotFound(
	g *gin.Context,
	responseHandler api.JSONResponseHandler,
	err error,
) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// error is logged in service
		_ = err
		responseHandler.NotFound(g)
		return false
	}
	return true
}

// handleAuthorizationError checks if the error is an authorization error
// if it is, a 403 response is sent
// if it is not, true is returned
func handleAuthorizationError(
	g *gin.Context,
	responseHandler api.JSONResponseHandler,
	err error,
) bool {
	if errors.Is(err, errs.ErrAuthorizationFailed) {
		// error is logged in service
		_ = err
		responseHandler.Forbidden(g)
		return false
	}
	return true
}

// handleValidationError checks if the error is a validation error
// if it is, a 400 response is sent
// if it is not, true is returned
func handleValidationError(
	g *gin.Context,
	responseHandler api.JSONResponseHandler,
	err error,
) bool {
	if errors.As(err, &errs.ValidationError{}) {
		// error is logged in service
		_ = err
		responseHandler.BadRequestMessage(g, err.Error())
		return false
	}
	return true
}

// handleCustomError checks if the error is a custom error
// if it is a 400 response is sent
// if it is not, true is returned
func handleCustomError(
	g *gin.Context,
	responseHandler api.JSONResponseHandler,
	err error,
) bool {
	if errors.As(err, &errs.CustomError{}) {
		// error is logged in service
		_ = err
		responseHandler.BadRequestMessage(g, err.Error())
		return false
	}
	return true
}

// handleServerError checks if the error is a server error
// if it is, a 500 response is sent
// if it is not, true is returned
func handleServerError(
	g *gin.Context,
	responseHandler api.JSONResponseHandler,
	err error,
) bool {
	if err != nil {
		// error is logged in service
		_ = err
		responseHandler.ServerError(g)
		return false
	}
	return true
}

func setSecureContentDisposition(c *gin.Context, filename string) {
	// Strip any directory components
	filename = filepath.Base(filename)

	// Remove any potentially problematic characters
	filename = strings.Map(func(r rune) rune {
		// Keep only alphanumeric, space, dash, underscore and dot
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			(r == ' ' || r == '-' || r == '_' || r == '.') {
			return r
		}
		return -1
	}, filename)

	// Ensure we still have a valid filename
	if filename == "" || filename == "." || filename == ".." {
		filename = time.Now().UTC().Format("20060102150405")
	}

	// Properly encode the filename for Content-Disposition
	encodedFilename := mime.QEncoding.Encode("utf-8", filename)

	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s";`,
			encodedFilename,
		),
	)
}

func (c *Common) requiresFlag(g *gin.Context, featureFlag string) {
	// handle session
	_, _, ok := c.handleSession(g)
	if !ok {
		return
	}
	c.Response.ServerErrorMessage(g, "requires "+featureFlag+" edition")
}
