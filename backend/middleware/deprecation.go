package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// DeprecationHeaders is the API half of the runtime-discoverability
// requirement in docs/deprecation-policy.md (MAINT-01, issue #490): a
// deprecated endpoint, method, or query parameter on the /api/v1 surface must
// announce itself on every response that touches it, not only in a changelog.
//
// It emits, per RFC 8594 (https://www.rfc-editor.org/rfc/rfc8594):
//
//   - Deprecation — "true" or "@<unix>", the flag itself;
//   - Sunset — the HTTP-date of the EARLIEST removal (the end of the window),
//     never a date inside it;
//   - Link — rel="deprecation" pointing at the policy page, plus
//     rel="successor-version" when a replacement exists.
//
// The set of deprecated elements lives in deprecatedEndpoints below. It is
// empty today — nothing on /api/v1 is deprecated (docs/deprecations.md, the
// 2026-09-08 retroactive audit) — so this middleware is inert. The first
// deprecation adds its entry there in the same change that adds its
// docs/deprecations.md register row and marks the element `deprecated: true`
// in backend/openapi.yaml.
//
// It is registered as a global middleware in main.go but only ever matches an
// /api/v1 route, so running it on every request is harmless while the registry
// is empty and cheap once it is not.
//
// Headers are set BEFORE the handler runs: gin has already resolved the route
// (c.FullPath() is populated) and the query string is available, and a header
// set after the handler has written the response body would be dropped.
func DeprecationHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, d := range deprecatedEndpoints {
			if d.matches(c) {
				d.apply(c)
				break
			}
		}
		c.Next()
	}
}

// deprecationPolicyURL is the published deprecation policy, emitted as the
// Link rel="deprecation" target.
const deprecationPolicyURL = "https://drewbrunning.github.io/mycorrhizal-crm/deprecation-policy"

// deprecatedEndpoint is one deprecated element of the /api/v1 surface. Every
// entry corresponds to exactly one row in docs/deprecations.md.
type deprecatedEndpoint struct {
	// Method is the HTTP method to match (e.g. "GET"). Empty matches any.
	Method string
	// FullPath is the gin route template to match, exactly as the route is
	// registered — e.g. "/api/v1/contacts/:id". Empty matches any path (only
	// sensible together with Param).
	FullPath string
	// Param, when set, narrows the match to requests that carry this query
	// parameter — for deprecating one parameter of an otherwise-current
	// endpoint.
	Param string
	// Deprecation is the Deprecation header value ("true" or "@<unix>").
	Deprecation string
	// Sunset is the RFC 1123 HTTP-date of the earliest removal.
	Sunset string
	// Successor, when set, is emitted as an additional Link
	// rel="successor-version" (an absolute URL or an /api/v1 path).
	Successor string
}

// deprecatedEndpoints is the live registry. Intentionally empty — see
// DeprecationHeaders' doc comment. setDeprecatedEndpoints swaps it for a
// fixture set and returns a restore func, so the failure-signal path stays
// exercised while the real registry is empty.
var deprecatedEndpoints []deprecatedEndpoint

// setDeprecatedEndpoints installs a fixture registry and returns a function
// that restores the previous one. Test-only seam.
func setDeprecatedEndpoints(fixture []deprecatedEndpoint) (restore func()) {
	prev := deprecatedEndpoints
	deprecatedEndpoints = fixture
	return func() { deprecatedEndpoints = prev }
}

// matches reports whether this entry applies to the current request.
func (d deprecatedEndpoint) matches(c *gin.Context) bool {
	if d.Method != "" && !strings.EqualFold(d.Method, c.Request.Method) {
		return false
	}
	if d.FullPath != "" && d.FullPath != c.FullPath() {
		return false
	}
	if d.Param != "" {
		if _, ok := c.GetQuery(d.Param); !ok {
			return false
		}
	}
	return true
}

// apply writes the RFC 8594 headers for a matched deprecation.
func (d deprecatedEndpoint) apply(c *gin.Context) {
	deprecation := d.Deprecation
	if deprecation == "" {
		deprecation = "true"
	}
	c.Header("Deprecation", deprecation)
	if d.Sunset != "" {
		c.Header("Sunset", d.Sunset)
	}
	links := []string{"<" + deprecationPolicyURL + ">; rel=\"deprecation\""}
	if d.Successor != "" {
		links = append(links, "<"+d.Successor+">; rel=\"successor-version\"")
	}
	c.Header("Link", strings.Join(links, ", "))
}
