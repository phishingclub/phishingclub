package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/utils"
	"go.uber.org/zap"
)

func NewAllowIPMiddleware(conf *config.Config, logger *zap.SugaredLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If no IP restrictions are configured, allow all
		if len(conf.IPSecurity.AdminAllowed) == 0 {
			c.Next()
			return
		}
		clientIP := c.ClientIP()
		allowed := utils.IPMatchesList(clientIP, conf.IPSecurity.AdminAllowed)

		if !allowed {
			logger.Infow("blocked unauthorized IP access attempt",
				"ip", clientIP)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
	}
}
