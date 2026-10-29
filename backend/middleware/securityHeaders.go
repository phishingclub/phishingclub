package middleware

import "github.com/gin-gonic/gin"

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		// A strict base CSP for every admin response. It sets no script-src or
		// style-src. When the single page app shell is served, serveIndex
		// replaces it with the full policy read from the SvelteKit csp meta tag,
		// which is stripped from the html, plus frame-ancestors and form-action.
		// If that meta can not be read the shell is served unchanged and the
		// meta carries the resource directives next to this header. The preview
		// handler replaces it with its own sandbox policy so the preview stays
		// frameable while every other response keeps frame-ancestors none.
		c.Header("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'")
		c.Next()
	}
}
