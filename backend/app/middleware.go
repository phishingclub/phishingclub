package app

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/middleware"
	"go.uber.org/zap"
)

// Middlwares is a collection of middlewares
type Middlewares struct {
	IPLimiter          gin.HandlerFunc
	LoginRateLimiter   gin.HandlerFunc
	SessionHandler     gin.HandlerFunc
	SoftSessionHandler gin.HandlerFunc
}

// NewMiddlewares creates a collection of middlewares
func NewMiddlewares(
	requestPerSecond float64,
	requestBurst int,
	conf *config.Config,
	services *Services,
	utils *Utilities,
	logger *zap.SugaredLogger,
) *Middlewares {
	ipLimiter := middleware.NewAllowIPMiddleware(conf, logger)
	loginThrottle := middleware.NewIPRateLimiterMiddleware(
		requestPerSecond, // requests per second
		requestBurst,     // burst
	)
	sessionHandler := middleware.NewSessionHandler(
		services.Session,
		services.User,
		utils.JSONResponseHandler,
		logger,
	)
	softSessionHandler := middleware.NewSoftSessionHandler(
		services.Session,
		services.User,
		logger,
	)

	return &Middlewares{
		IPLimiter:          ipLimiter,
		LoginRateLimiter:   loginThrottle,
		SessionHandler:     sessionHandler,
		SoftSessionHandler: softSessionHandler,
	}
}

// ExtendedTimeout returns a middleware that extends the write deadline for long-running operations.
func (m *Middlewares) ExtendedTimeout(timeout time.Duration) gin.HandlerFunc {
	return middleware.ExtendedTimeout(timeout)
}
