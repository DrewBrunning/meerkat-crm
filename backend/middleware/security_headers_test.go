package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		enableHSTS bool
	}{
		{name: "HSTS disabled (plain HTTP)", enableHSTS: false},
		{name: "HSTS enabled (HTTPS)", enableHSTS: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(SecurityHeadersMiddleware(tt.enableHSTS))
			router.GET("/test", func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
			assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
			assert.Equal(t, "default-src 'none'; frame-ancestors 'none'", w.Header().Get("Content-Security-Policy"))
			assert.Equal(t, "camera=(), microphone=(), geolocation=(), interest-cohort=()", w.Header().Get("Permissions-Policy"))

			if tt.enableHSTS {
				assert.Equal(t, "max-age=31536000; includeSubDomains", w.Header().Get("Strict-Transport-Security"))
			} else {
				assert.Empty(t, w.Header().Get("Strict-Transport-Security"))
			}
		})
	}
}

// Issue #872: authenticated /api/v1 JSON responses must carry
// Cache-Control: no-store so a shared/intermediary cache or the browser
// bfcache cannot retain one context's view of private data. Scoped to the
// /api/ prefix; everything else is left alone (this process serves no static
// assets, but the scoping is what keeps it that way if that ever changes).
func TestSecurityHeadersMiddleware_CacheControlNoStoreOnAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	apiPaths := []string{"/api/v1/contacts", "/api/v1/users/me", "/api/v1/audit", "/api/v2/anything"}
	for _, p := range apiPaths {
		for _, hsts := range []bool{false, true} {
			t.Run(p, func(t *testing.T) {
				router := gin.New()
				router.Use(SecurityHeadersMiddleware(hsts))
				router.GET("/*any", func(c *gin.Context) { c.Status(http.StatusOK) })

				req := httptest.NewRequest(http.MethodGet, p, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				assert.Equal(t, "no-store", w.Header().Get("Cache-Control"),
					"%s must carry Cache-Control: no-store", p)
			})
		}
	}
}

// The header is set before the handler runs, so it lands even on an error
// response — a cached 4xx/5xx carrying a private-data fragment is the same risk.
func TestSecurityHeadersMiddleware_CacheControlNoStoreOnAPIError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(SecurityHeadersMiddleware(false))
	router.GET("/api/v1/contacts/:id", func(c *gin.Context) { c.Status(http.StatusNotFound) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/contacts/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

// Non-/api/ paths (health probes, and any static asset a future edge might
// route through this process) must NOT get no-store — a hashed, ETag'd SPA
// asset stays cacheable.
func TestSecurityHeadersMiddleware_NoCacheControlOffAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, p := range []string{"/health", "/metrics", "/assets/app.4f2a.js", "/", "/apiary"} {
		t.Run(p, func(t *testing.T) {
			router := gin.New()
			router.Use(SecurityHeadersMiddleware(false))
			router.GET("/*any", func(c *gin.Context) { c.Status(http.StatusOK) })

			req := httptest.NewRequest(http.MethodGet, p, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Empty(t, w.Header().Get("Cache-Control"),
				"%s must not have Cache-Control forced by the security middleware", p)
			// The rest of the security headers still apply everywhere.
			assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
		})
	}
}

func TestSecurityHeadersMiddleware_CSPAppliesToAllResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The CSP is defense-in-depth for any response, including error responses
	// and non-JSON payloads (e.g. the image proxy) - verify it isn't skipped
	// on a non-2xx response.
	router := gin.New()
	router.Use(SecurityHeadersMiddleware(false))
	router.GET("/not-found", func(c *gin.Context) {
		c.Status(http.StatusNotFound)
	})

	req := httptest.NewRequest(http.MethodGet, "/not-found", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, "default-src 'none'; frame-ancestors 'none'", w.Header().Get("Content-Security-Policy"))
	assert.Equal(t, "camera=(), microphone=(), geolocation=(), interest-cohort=()", w.Header().Get("Permissions-Policy"))
}

// TestNginxConfigsKeepPermissionsPolicyInStep pins the SPA nginx configs to the
// same Permissions-Policy value the Go backend emits. The two nginx files are
// explicitly documented as "kept in step" with one another (frontend/nginx.conf
// serves the split image, docker/nginx.conf the all-in-one image); a fix applied
// to only one leaves half the deployments with the header missing. It also pins
// the HSTS include to every add_header block in docker/nginx.conf, since nginx
// does not inherit add_header across location blocks.
func TestNginxConfigsKeepPermissionsPolicyInStep(t *testing.T) {
	want := `add_header Permissions-Policy "` + permissionsPolicy + `" always;`
	repoRoot := filepath.Join("..", "..")

	for _, rel := range []string{
		filepath.Join("docker", "nginx.conf"),
		filepath.Join("frontend", "nginx.conf"),
	} {
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		require.NoErrorf(t, err, "read %s", rel)
		content := string(b)

		xfo := strings.Count(content, `add_header X-Frame-Options "DENY" always;`)
		pp := strings.Count(content, want)
		assert.Equalf(t, xfo, pp,
			"%s: Permissions-Policy must be repeated in every add_header block (found %d X-Frame-Options, %d Permissions-Policy)", rel, xfo, pp)
	}

	b, err := os.ReadFile(filepath.Join(repoRoot, "docker", "nginx.conf"))
	require.NoError(t, err)
	content := string(b)
	xfo := strings.Count(content, `add_header X-Frame-Options "DENY" always;`)
	hsts := strings.Count(content, "include /etc/nginx/hsts.conf;")
	assert.Equalf(t, xfo, hsts,
		"docker/nginx.conf: the HSTS include must be repeated in every add_header block (found %d X-Frame-Options, %d HSTS includes)", xfo, hsts)
}
