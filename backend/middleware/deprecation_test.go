package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func deprecationRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(DeprecationHeaders())
	v1.GET("/contacts", func(c *gin.Context) { c.Status(http.StatusOK) })
	v1.GET("/contacts/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func serve(r *gin.Engine, method, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestDeprecationHeaders_InertWhenRegistryEmpty pins the real state: with the
// committed (empty) deprecatedEndpoints, no response carries a deprecation
// signal.
func TestDeprecationHeaders_InertWhenRegistryEmpty(t *testing.T) {
	w := serve(deprecationRouter(), http.MethodGet, "/api/v1/contacts")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("Deprecation"))
	assert.Empty(t, w.Header().Get("Sunset"))
	assert.Empty(t, w.Header().Get("Link"))
}

func TestDeprecationHeaders_MatchedEndpoint(t *testing.T) {
	defer setDeprecatedEndpoints([]deprecatedEndpoint{{
		Method:      "GET",
		FullPath:    "/api/v1/contacts/:id",
		Deprecation: "true",
		Sunset:      "Wed, 10 Dec 2026 00:00:00 GMT",
		Successor:   "/api/v1/people/:id",
	}})()

	r := deprecationRouter()

	w := serve(r, http.MethodGet, "/api/v1/contacts/42")
	assert.Equal(t, "true", w.Header().Get("Deprecation"))
	assert.Equal(t, "Wed, 10 Dec 2026 00:00:00 GMT", w.Header().Get("Sunset"))
	link := w.Header().Get("Link")
	assert.Contains(t, link, `rel="deprecation"`)
	assert.Contains(t, link, deprecationPolicyURL)
	assert.Contains(t, link, `<`+`/api/v1/people/:id>; rel="successor-version"`)

	// A sibling route that is not in the registry stays clean.
	w = serve(r, http.MethodGet, "/api/v1/contacts")
	assert.Empty(t, w.Header().Get("Deprecation"))
}

func TestDeprecationHeaders_MethodNarrowing(t *testing.T) {
	defer setDeprecatedEndpoints([]deprecatedEndpoint{{
		Method:      "POST",
		FullPath:    "/api/v1/contacts",
		Deprecation: "true",
	}})()

	// The registered route is GET; a GET must not match a POST-scoped entry.
	w := serve(deprecationRouter(), http.MethodGet, "/api/v1/contacts")
	assert.Empty(t, w.Header().Get("Deprecation"))
}

func TestDeprecationHeaders_ParamScoped(t *testing.T) {
	defer setDeprecatedEndpoints([]deprecatedEndpoint{{
		FullPath:    "/api/v1/contacts",
		Param:       "legacy_sort",
		Deprecation: "true",
		Sunset:      "Wed, 10 Dec 2026 00:00:00 GMT",
	}})()

	r := deprecationRouter()

	// Without the deprecated parameter: clean.
	assert.Empty(t, serve(r, http.MethodGet, "/api/v1/contacts").Header().Get("Deprecation"))

	// With it: signalled, and no successor Link when none is configured.
	w := serve(r, http.MethodGet, "/api/v1/contacts?legacy_sort=name")
	assert.Equal(t, "true", w.Header().Get("Deprecation"))
	assert.NotContains(t, w.Header().Get("Link"), "successor-version")
}

func TestDeprecationHeaders_DefaultDeprecationValue(t *testing.T) {
	defer setDeprecatedEndpoints([]deprecatedEndpoint{{
		FullPath: "/api/v1/contacts",
	}})()

	w := serve(deprecationRouter(), http.MethodGet, "/api/v1/contacts")
	assert.Equal(t, "true", w.Header().Get("Deprecation"))
}
