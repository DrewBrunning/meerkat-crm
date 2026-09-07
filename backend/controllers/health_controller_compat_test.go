package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The /health compatibility contract (issue #528,
// docs/client-compatibility-policy.md): the server advertises the API
// contract generation it speaks and — only when a floor has actually been
// raised — the oldest client version it still supports. Absent means no floor
// has ever been declared, which is the policy's default and must stay the
// default for every unconfigured server.
func TestHealthCheck_AdvertisesAPIContractVersionAlways(t *testing.T) {
	_, _, r := migratedHealthRouter(t)

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusOK, code)

	require.Equal(t, "v1", body["api_contract_version"],
		"/health must always advertise the API contract generation (\"v1\" while the API is on /api/v1)")
}

func TestHealthCheck_OmitsMinClientVersionByDefault(t *testing.T) {
	_, _, r := migratedHealthRouter(t)

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusOK, code)

	// The default posture: no floor has ever been raised, so the field must
	// be absent (not present-but-empty), so an old client cannot mistake an
	// empty string for a "no constraint" that a parse failure might block on.
	_, present := body["min_client_version"]
	assert.False(t, present, "an unconfigured server must not advertise a min_client_version")
}

func TestHealthCheck_ReportsConfiguredMinClientVersion(t *testing.T) {
	_, cfg, r := migratedHealthRouter(t)
	cfg.MinClientVersion = "0.6.0"

	code, body := getJSON(t, r, "/health")
	require.Equal(t, http.StatusOK, code)

	require.Equal(t, "0.6.0", body["min_client_version"])
}

// The compatibility fields must be part of the typed response too (not just
// the raw map), so a client deserializing into the Go struct sees them.
func TestHealthCheck_CompatibilityFieldsSurviveTypedDecode(t *testing.T) {
	_, cfg, r := migratedHealthRouter(t)
	cfg.MinClientVersion = "0.7.0"

	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp HealthResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "0.7.0", resp.MinClientVersion)
	assert.Equal(t, "v1", resp.APIContractVersion)
}
