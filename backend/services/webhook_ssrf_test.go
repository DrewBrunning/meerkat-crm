package services

import (
	"context"
	"testing"

	"mycorrhizal/config"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The guard is opt-in: self-hosted installs legitimately deliver webhooks to
// other services on the same host, so the default must stay unfiltered.
func TestClientForRespectsBlockPrivateURLs(t *testing.T) {
	assert.Same(t, deliveryClient, clientFor(config.Config{WebhookBlockPrivateURLs: false}),
		"default config must use the unfiltered client")
	assert.Same(t, guardedDeliveryClient, clientFor(config.Config{WebhookBlockPrivateURLs: true}),
		"WEBHOOK_BLOCK_PRIVATE_URLS must select the guarded client")
}

// Regression test for the original bypass: the guard used to be a pre-flight
// DNS check on the configured URL only, so a webhook pointed at a host that
// redirected to an internal address reached it anyway. Enforcement now lives in
// the transport's dialer, which every connection goes through — including ones
// opened for redirect targets.
func TestGuardedClientBlocksInternalAddresses(t *testing.T) {
	transport := guardedDeliveryClient.Transport
	require.NotNil(t, transport, "guarded client must not fall back to the default transport")

	for _, target := range []string{
		"http://127.0.0.1:1/hook",
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"http://10.0.0.1/hook",
		"http://100.64.0.1/hook", // CGNAT
	} {
		t.Run(target, func(t *testing.T) {
			resp, err := guardedDeliveryClient.Get(target)
			if resp != nil {
				resp.Body.Close()
			}
			require.Error(t, err, "guarded client must refuse %s", target)
			assert.ErrorIs(t, err, ErrWebhookPrivateAddress)
		})
	}
}

// Issue #869: a transport-level delivery failure must not reflect the raw Go
// dial error. The stored delivery record is echoed to any authenticated user
// through GET /api/v1/webhooks (delivery-health rollup) and
// POST /api/v1/webhooks/:id/test (delivery.error) — so a raw
// "dial tcp 127.0.0.1:1: connect: connection refused" is an internal
// port-scan / service-existence oracle. The stored Error must be generic and
// carry no host, port, or refused/timeout/no-host wording.
func TestDeliverWebhookTransportErrorIsNotAPortScanOracle(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	// Nothing listens on 127.0.0.1:1, so clientFor(cfg).Do fails with a
	// *url.Error wrapping "dial tcp 127.0.0.1:1: connect: connection refused".
	wh := newTestWebhook("http://127.0.0.1:1/hook", "secret")
	require.NoError(t, db.Create(&wh).Error)

	delivery := deliverWebhook(context.Background(), db, config.Config{WebhookBlockPrivateURLs: false},
		wh, "contact.created", []byte(`{}`), 1)

	require.NotNil(t, delivery.Error)
	assert.Equal(t, genericDeliveryTransportError, *delivery.Error,
		"a transport failure must store the generic string, not the Go dial error")

	// The persisted record is what the webhook API actually serves back.
	var loaded models.WebhookDelivery
	require.NoError(t, db.First(&loaded, delivery.ID).Error)
	require.NotNil(t, loaded.Error)
	stored := *loaded.Error
	assert.Equal(t, genericDeliveryTransportError, stored)
	for _, leak := range []string{
		"127.0.0.1", "1:", ":1", "connection refused", "dial tcp", "connect:",
		"no such host", "timeout", "i/o timeout", "refused", "/hook",
	} {
		assert.NotContainsf(t, stored, leak,
			"stored webhook delivery error leaks %q — internal port-scan oracle (issue #869)", leak)
	}
}

// The http.NewRequest error branch (a URL malformed enough that request
// construction itself fails) must likewise not echo the raw parser error,
// which repeats the offending URL back to the caller.
func TestDeliverWebhookInvalidURLErrorIsGeneric(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	wh := newTestWebhook("http://[::1", "secret") // unbalanced IPv6 bracket
	require.NoError(t, db.Create(&wh).Error)

	delivery := deliverWebhook(context.Background(), db, config.Config{WebhookBlockPrivateURLs: false},
		wh, "contact.created", []byte(`{}`), 1)

	require.NotNil(t, delivery.Error)
	assert.Equal(t, genericDeliveryInvalidURL, *delivery.Error)
	assert.NotContains(t, *delivery.Error, "[::1")
}

func TestIsPrivateURLFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		private bool
	}{
		{"loopback", "http://127.0.0.1/hook", true},
		{"metadata", "http://169.254.169.254/", true},
		{"private range", "http://10.1.2.3/hook", true},
		{"CGNAT", "http://100.64.0.1/hook", true},
		{"unparseable URL", "http://[::1", true},
		{"unresolvable host", "http://nonexistent.invalid/hook", true},
		{"public IP literal", "http://8.8.8.8/hook", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.private, isPrivateURL(tt.url))
		})
	}
}
