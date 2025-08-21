package api

import (
	"fmt"
	"net/http"

	"github.com/go-errors/errors"

	"github.com/gin-gonic/gin"
)

const (
	// All constant here are used for frontend responses
	NotFound                   = "Not found"
	InvalidData                = "Missing or invalid data"
	Unauthorized               = "Authorization failed"
	Forbidden                  = "Access denied"
	ServerError                = "Internal server error"
	InvalidCompanyID           = "Invalid company ID"
	InvalidDomainID            = "Invalid domain ID"
	InvalidMessageID           = "Invalid message ID"
	InvalidPageID              = "Invalid page ID"
	InvalidPageTypeID          = "Invalid page type ID"
	InvalidRecipientID         = "Invalid recipient ID"
	InvalidRecipientGroupID    = "Invalid recipient group ID"
	InvalidSMTPConfigurationID = "Invalid SMTP configuration ID"
	CompanyNotFound            = "Company not found"
)

// JSONResponse is the response structure for the API
type JSONResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data"`
	Error   string `json:"error"`
}

// JSONResponseHandler is a interface for API responses
type JSONResponseHandler interface {
	OK(g *gin.Context, data any)
	NotFound(g *gin.Context)
	Unauthorized(g *gin.Context)
	Forbidden(g *gin.Context)
	BadRequest(g *gin.Context)
	BadRequestMessage(g *gin.Context, message string)
	ValidationFailed(g *gin.Context, field string, err error)
	ServerError(g *gin.Context)
	ServerErrorMessage(g *gin.Context, message string)
}

// jsonResponseHandler is a JSON API responder
type jsonResponseHandler struct{}

// NewJSONResponseHandler creates a new JSON responder
func NewJSONResponseHandler() JSONResponseHandler {
	return &jsonResponseHandler{}
}

// newResponse creates a new JSON response
func (r *jsonResponseHandler) newResponse(
	success bool,
	data any,
	errorMessage string,
) JSONResponse {
	return JSONResponse{
		Success: success,
		Data:    data,
		Error:   errorMessage,
	}
}

// newOK creates a new OK response
func (r *jsonResponseHandler) newOK(data any) JSONResponse {
	return r.newResponse(true, data, "")
}

// newError creates a new error response
func (r *jsonResponseHandler) newError(errorMessage string) JSONResponse {
	return r.newResponse(false, nil, errorMessage)
}

// OK responds with 200 - OK
func (r *jsonResponseHandler) OK(g *gin.Context, data any) {
	g.JSON(http.StatusOK, r.newOK(data))
}

// NotFound responds 404 - NOT FOUND
func (r *jsonResponseHandler) NotFound(g *gin.Context) {
	g.JSON(
		http.StatusNotFound,
		r.newError(NotFound),
	)
	g.Abort()
}

// Unauthorized responds with 401 - UNAUTHORIZED
// generic error handler for authentication errors
func (r *jsonResponseHandler) Unauthorized(g *gin.Context) {
	g.JSON(
		http.StatusUnauthorized,
		r.newError(Forbidden),
	)
	g.Abort()
}

// Forbidden responds with 403 - FORBIDDEN and a custom error message
// generic error handler for authorization errors
func (r *jsonResponseHandler) Forbidden(g *gin.Context) {
	g.JSON(
		http.StatusForbidden,
		r.newError(Unauthorized),
	)
	g.Abort()
}

// BadRequest responds with 400 - BAD REQUEST
func (r *jsonResponseHandler) BadRequest(g *gin.Context) {
	g.JSON(
		http.StatusBadRequest,
		r.newError(InvalidData),
	)
	g.Abort()
}

// BadRequestMessage responds with 400 - BAD REQUEST and a custom error message
func (r *jsonResponseHandler) BadRequestMessage(g *gin.Context, message string) {
	g.JSON(
		http.StatusBadRequest,
		r.newError(message),
	)
	g.Abort()
}

func (r *jsonResponseHandler) unwrapErrorMessage(err error) string {
	message := err.Error()
	unwrapped := errors.Unwrap(err)
	if unwrapped != nil {
		message = r.unwrapErrorMessage(unwrapped)
	}
	return message
}

// ValidationFailed responds with 400 - BAD REQUEST and a validation error message
// that includes the field name and the validation error message
// if the err IS a ValidationError it will unwrap the validation error
// else it will use the error passed
func (r *jsonResponseHandler) ValidationFailed(g *gin.Context, field string, err error) {
	message := r.unwrapErrorMessage(err)
	g.JSON(
		http.StatusBadRequest,
		r.newError(
			fmt.Sprintf("%s %s", field, message),
		),
	)
	g.Abort()
}

// ServerError responds with 500 - INTERNAL SERVER ERROR
func (r *jsonResponseHandler) ServerError(g *gin.Context) {
	g.JSON(
		http.StatusInternalServerError,
		r.newError(ServerError),
	)
	g.Abort()
}

// ServerError responds with 500 - INTERNAL SERVER ERROR and a custom error message
func (r *jsonResponseHandler) ServerErrorMessage(g *gin.Context, message string) {
	g.JSON(
		http.StatusInternalServerError,
		r.newError(message),
	)
	g.Abort()
}
