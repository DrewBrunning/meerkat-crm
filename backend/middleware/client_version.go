package middleware

import (
	"strconv"
	"strings"

	"mycorrhizal/config"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/logger"

	"github.com/gin-gonic/gin"
)

// ClientVersionHeader is the request header a native client uses to tell the
// server its own versionName (issue #692). It is the one-direction auth
// exchange the compatibility policy calls for (docs/client-compatibility-policy.md):
// the client sends its version, the server logs it and — when MIN_CLIENT_VERSION
// declares a floor — refuses to authenticate a build below it. /health remains
// the single source of truth for the server version on the client; nothing here
// echoes a server version back.
//
// Naming: X-Client-Version (kebab) matches the other custom REST headers this
// server already reads (X-Request-ID, Idempotency-Key) rather than inventing a
// second casing convention.
const ClientVersionHeader = "X-Client-Version"

// EnforceMinClientVersion refuses session-minting requests from clients below
// the configured MIN_CLIENT_VERSION floor (the authoritative backstop of issue
// #692). It is inert when no floor is declared (the policy default — no floor
// ever raised means every client is compatible).
//
// Rejection happens BEFORE any user authentication work: no credential check,
// no per-account lockout accounting, no user lookup. A stranded client must not
// be able to probe credentials or enumerate accounts through the floor check,
// and must not burn a real account's failed-attempt budget.
//
// The header value is parsed strictly as `major.minor.patch` (the issue's
// "invalid version strings are treated as invalid" rule): an absent or
// non-conforming header is rejected once a floor exists, because a client that
// cannot prove it is at or above the floor cannot be served. This is what makes
// the check authoritative — a modified client that skips its own client-side
// gate is still refused here.
func EnforceMinClientVersion(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		floor := strings.TrimSpace(cfg.MinClientVersion)
		if floor == "" {
			c.Next()
			return
		}
		floorTriple, ok := parseClientVersionTriple(floor)
		if !ok {
			// Unreachable in production: config.Validate() refuses to boot on a
			// malformed MIN_CLIENT_VERSION. Fail closed defensively rather than
			// silently serving every client.
			logger.FromContext(c).Error().
				Str("min_client_version", floor).
				Msg("MIN_CLIENT_VERSION floor is not parseable; refusing request")
			rejectClientVersion(c, floor)
			return
		}

		raw := strings.TrimSpace(c.GetHeader(ClientVersionHeader))
		clientTriple, strictOK := parseStrictClientVersion(raw)
		if !strictOK {
			logger.FromContext(c).Warn().
				Str("client_version", logger.SanitizeLogField(raw)).
				Str("min_client_version", floor).
				Str("path", c.Request.URL.Path).
				Msg("rejected authentication: client version absent or not a major.minor.patch version")
			rejectClientVersion(c, floor)
			return
		}

		if versionTripleLess(clientTriple, floorTriple) {
			logger.FromContext(c).Warn().
				Str("client_version", raw).
				Str("min_client_version", floor).
				Str("path", c.Request.URL.Path).
				Msg("rejected authentication: client version below the configured floor")
			rejectClientVersion(c, floor)
			return
		}

		c.Next()
	}
}

// rejectClientVersion answers the request with the structured CLIENT_NOT_SUPPORTED
// error. It aborts the chain, so no handler that follows can touch credentials.
func rejectClientVersion(c *gin.Context, requiredVersion string) {
	apperrors.AbortWithError(c, apperrors.ErrClientNotSupported(requiredVersion))
}

// parseStrictClientVersion parses a client-supplied version that must be
// exactly `major.minor.patch` of decimal digits — the issue #692 rule that
// anything else is an invalid version. Returns ok=false for an empty string, a
// leading "v", a prerelease/build suffix, or any non-numeric segment.
func parseStrictClientVersion(raw string) (triple, bool) {
	segments := strings.Split(raw, ".")
	if len(segments) != 3 {
		return triple{}, false
	}
	var numbers [3]int
	for i, segment := range segments {
		if segment == "" || !isAllDigits(segment) {
			return triple{}, false
		}
		n, err := strconv.Atoi(segment)
		if err != nil {
			return triple{}, false
		}
		numbers[i] = n
	}
	return triple{major: numbers[0], minor: numbers[1], patch: numbers[2]}, true
}

// parseClientVersionTriple parses the operator-configured floor. The floor is
// validated at boot by config.Validate() against a looser shape (major,
// major.minor or major.minor.patch, optional v/-prerelease/+build suffix), so
// this tolerant parse mirrors that shape and reduces it to the numeric triple
// the comparison uses.
func parseClientVersionTriple(raw string) (triple, bool) {
	core := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	core = strings.TrimSpace(core)
	if core == "" {
		return triple{}, false
	}
	core = strings.SplitN(core, "-", 2)[0]
	core = strings.SplitN(core, "+", 2)[0]
	segments := strings.Split(core, ".")
	if len(segments) < 1 || len(segments) > 3 {
		return triple{}, false
	}
	var numbers [3]int
	for i, segment := range segments {
		if segment == "" || !isAllDigits(segment) {
			return triple{}, false
		}
		n, err := strconv.Atoi(segment)
		if err != nil {
			return triple{}, false
		}
		numbers[i] = n
	}
	return triple{major: numbers[0], minor: numbers[1], patch: numbers[2]}, true
}

// triple is a parsed major.minor.patch version reduced to comparable integers.
type triple struct {
	major, minor, patch int
}

// versionTripleLess reports whether a < b as numeric major.minor.patch.
func versionTripleLess(a, b triple) bool {
	if a.major != b.major {
		return a.major < b.major
	}
	if a.minor != b.minor {
		return a.minor < b.minor
	}
	return a.patch < b.patch
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
