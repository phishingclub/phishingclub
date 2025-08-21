package middleware

import (
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/phishingclub/phishingclub/config"
	"go.uber.org/zap"
)

func NewAllowIPMiddleware(conf *config.Config, logger *zap.SugaredLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// If no IP restrictions are configured, allow all
		if len(conf.IPSecurity.AdminAllowed) == 0 {
			c.Next()
			return
		}
		c.RemoteIP()
		clientIP := c.ClientIP()
		allowed := false
		for _, allowedIP := range conf.IPSecurity.AdminAllowed {
			// check if the allowed entry is a CIDR
			if strings.Contains(allowedIP, "/") {
				_, ipNet, err := net.ParseCIDR(allowedIP)
				if err != nil {
					logger.Errorw("Invalid CIDR in allowed IPs",
						"cidr", allowedIP,
						"error", err)
					continue
				}

				ip := net.ParseIP(clientIP)
				if ipNet.Contains(ip) {
					allowed = true
					break
				}
			} else {
				// Direct IP comparison
				if clientIP == allowedIP {
					allowed = true
					break
				}
			}
		}

		if !allowed {
			logger.Infow("blocked unauthorized IP access attempt",
				"ip", clientIP)
			c.AbortWithStatus(http.StatusForbidden)
			return
		}

		c.Next()
	}
}
