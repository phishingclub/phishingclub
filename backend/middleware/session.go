package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/api"
	"github.com/phishingclub/phishingclub/controller"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/service"
	"go.uber.org/zap"
)

// NewSessionHandler creates a middleware that authenticates the user
// by checking it has a session, and if it does, it extends the session and puts
// the user and the session in the gin context.
// if the user does not have a session or must renew password, it returns an unauthorized response.
// if the request contains a valid user API key, the entire session handling is skipped
func NewSessionHandler(
	sessionService *service.Session,
	userService *service.User,
	responseHandler api.JSONResponseHandler,
	logger *zap.SugaredLogger,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		isValidAPISession := handleAPISession(c, userService, logger)
		if isValidAPISession {
			return
		}
		s, err := sessionService.GetAndExtendSession(c)
		if err != nil {
			// errors are logged in service
			_ = err
			responseHandler.Unauthorized(c)
			return
		}
		user := s.User
		if user == nil {
			logger.Error("user not found in session")
			responseHandler.Unauthorized(c)
			return
		}
		controller.SetSessionInGinContext(c, s)
		c.Next()
	}
}

// handleAPISession handles if there is a API token in the request header
// returns true if this was a valid API session request
func handleAPISession(
	c *gin.Context,
	userService *service.User,
	logger *zap.SugaredLogger,
) bool {
	if headerAPIKey := c.Request.Header.Get(data.APIHeaderKey); len(headerAPIKey) > 0 {
		// to check API apiUsers in constant time, we have to retrieve them all
		// hash them all and constant time check.
		apiUsers, err := userService.GetAllAPIKeysSHA256(c)
		if err != nil {
			logger.Error("failed to get all api key hashes")
			// responseHandler.BadRequest(c)
			return false
		}
		incomingHash := sha256.Sum256([]byte(headerAPIKey))
		found := false
		// Must check ALL keys in constant time
		var rApiUser *model.APIUser
		for _, apiUser := range apiUsers {
			if subtle.ConstantTimeCompare(incomingHash[:], apiUser.APIKeyHash[:]) == 1 {
				found = true
				rApiUser = apiUser
				break
			}
		}
		if !found {
			logger.Debug("API key not found")
			// responseHandler.Unauthorized(c)
			return false
		}
		// get user
		systemService, err := model.NewSystemSession()
		if err != nil {
			logger.Error("failed to get system user")
			return false
		}
		user, err := userService.GetByID(c, systemService, rApiUser.ID)
		if err != nil {
			logger.Error("failed to get user from API token")
			return false
		}
		now := time.Now()
		t := now.Add(time.Duration(1 * time.Minute)).UTC()
		expiresAt := &t
		maxAgeAt := &t
		sid := uuid.MustParse(data.APISessionID)
		session := &model.Session{
			ID:                &sid,
			ExpiresAt:         expiresAt,
			MaxAgeAt:          maxAgeAt,
			IP:                c.ClientIP(),
			User:              user,
			IsUserLoaded:      true,
			IsAPITokenRequest: true,
		}
		controller.SetSessionInGinContext(c, session)
		c.Next()
		return true
	}
	return false
}
