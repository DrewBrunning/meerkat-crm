package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// contentSecurityPolicy is the CSP applied to every response from this server.
//
// This Go process is a pure JSON/CardDAV API: it never renders or serves the
// React frontend (that's nginx's job in the production image, see /Dockerfile
// and docker/nginx.conf, which ships its own CSP tuned for the SPA — MUI's
// 'unsafe-inline' styles, self-hosted /fonts/, etc. all live in that policy,
// not this one). Nothing this process returns is meant to execute as an active
// document, so the policy here can — and should — be maximally restrictive
// rather than mirroring the frontend's policy.
//
// "default-src 'none'; frame-ancestors 'none'" blocks every fetch directive
// (script, style, img, connect, font, ...) and framing outright. This is
// primarily defense-in-depth: it protects the few endpoints that return
// browser-renderable bytes (e.g. the image proxy in photo_controller.go)
// against being loaded as a top-level document or embedded in an active
// context, without having to special-case every response type.
const contentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'"

// permissionsPolicy gates browser feature permissions this product never uses
// and opts out of FLoC interest cohorts. It applies to every response; the SPA
// served by nginx repeats the same policy for the frontend (see
// docker/nginx.conf, which must be kept in step).
const permissionsPolicy = "camera=(), microphone=(), geolocation=(), interest-cohort=()"

//	Sets common HTTP security headers on all responses.
//
// enableHSTS should only be true when the server is reached via HTTPS
// (e.g. behind a TLS-terminating proxy), otherwise browsers may refuse
// plain-HTTP access for the duration of the max-age.
func SecurityHeadersMiddleware(enableHSTS bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", contentSecurityPolicy)
		c.Header("Permissions-Policy", permissionsPolicy)
		if enableHSTS {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Private API responses must never be retained by a shared/intermediary
		// cache or the browser bfcache: a heuristically-cached GET could serve
		// one context's view of private data to another (issue #872). Scoped to
		// /api/ so the SPA's static assets — served by nginx, not this process,
		// with their own ETag + hashed-filename long-cache — are untouched. This
		// process serves no static assets, so the prefix is the whole boundary.
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-store")
		}

		c.Next()
	}
}
