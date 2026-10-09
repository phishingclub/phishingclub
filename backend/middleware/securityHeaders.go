package middleware

import "github.com/gin-gonic/gin"

func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		// The built single page app ships its own CSP as a meta tag inside
		// index.html: SvelteKit hash mode hashes its inline bootstrap script at
		// build time and bakes the script and its matching hash together, which a
		// static file can only carry in the document, not as a response header.
		// That meta holds the resource directives (script-src with the hash,
		// style-src, img-src and so on). This header adds only what a meta tag can
		// not express, frame-ancestors and form-action, plus object-src and
		// base-uri. It sets no script-src or style-src, so it never conflicts with
		// the baked hash. It is applied to every admin response, including the
		// single page app fallback on unmatched paths. The preview handler
		// replaces this header with its own sandbox policy on its authenticated
		// response, so the preview stays frameable while every other path keeps
		// frame-ancestors none.
		c.Header("Content-Security-Policy", "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'")
		c.Next()
	}
}
