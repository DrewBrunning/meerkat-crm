package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"mycorrhizal/config"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//, re-dispatching
// Phase 3a of docs/adrs/0003-golden-fixtures-external-test-oracle.md: computeSignature,
// retryAt, saveDelivery, deliverWebhook, TriggerWebhooks and
// TestWebhookDelivery were all at 0% (or near it) coverage. This file covers
// the delivery path itself (signing, backoff, persistence, actual HTTP
// delivery, and the exported entry points). SSRF-guarding behavior
// (isPrivateURL, clientFor, the guarded dialer) is already covered by
// webhook_ssrf_test.go and is intentionally not duplicated here.

// ---- computeSignature ----------------------------------------------------

func TestComputeSignatureMatchesHMACSHA256Hex(t *testing.T) {
	secret := "top-secret"
	body := []byte(`{"hello":"world"}`)

	got := computeSignature(secret, body)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := fmt.Sprintf("%x", mac.Sum(nil))

	assert.Equal(t, want, got)
	// Sanity: hex-encoded SHA-256 HMAC is always 64 hex chars.
	assert.Len(t, got, 64)
}

func TestComputeSignatureDiffersOnSecretOrBody(t *testing.T) {
	base := computeSignature("secret-a", []byte("payload"))

	assert.NotEqual(t, base, computeSignature("secret-b", []byte("payload")),
		"signature must depend on the secret")
	assert.NotEqual(t, base, computeSignature("secret-a", []byte("different-payload")),
		"signature must depend on the body")
}

// TestComputeSignatureHandVerification is the required hand-verification for
// security-adjacent code: it recomputes the signature the way a receiving
// webhook consumer would (independently, via stdlib hmac/sha256) and confirms
// it matches computeSignature's output byte-for-byte. If computeSignature's
// secret or hash algorithm silently changed, this test — and the assertion
// used against the live HTTP header in TestDeliverWebhookSendsValidSignature
// below — would fail.
func TestComputeSignatureHandVerification(t *testing.T) {
	secret := "receiver-side-secret"
	body := []byte(`{"id":"abc","event":"contact.created"}`)

	got := computeSignature(secret, body)

	independentMAC := hmac.New(sha256.New, []byte(secret))
	independentMAC.Write(body)
	independentHex := fmt.Sprintf("%x", independentMAC.Sum(nil))

	require.Equal(t, independentHex, got,
		"a receiving webhook consumer verifying HMAC-SHA256 over the raw body with the shared secret must accept this signature")
}

// ---- retryAt (webhookRetryPolicy — INT-03 #466) --------------------------

func TestRetryAtSchedule(t *testing.T) {
	before := time.Now()
	first := retryAt(1, 0)
	second := retryAt(2, 0)
	third := retryAt(3, 0) // budget of maxDeliveryAttempts (3) is spent
	after := time.Now()

	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.Nil(t, third, "attempts at/over the retry budget must not be retried")

	// attempt 1 -> ~BaseDelay (5m), within the ±20% jitter window.
	base := webhookRetryPolicy.BaseDelay
	assertRetryWithin(t, first, before, after, time.Duration(float64(base)*0.8), time.Duration(float64(base)*1.2))

	// attempt 2 -> ~BaseDelay*Multiplier (15m), jittered.
	step2 := time.Duration(float64(base) * webhookRetryPolicy.Multiplier)
	assertRetryWithin(t, second, before, after, time.Duration(float64(step2)*0.8), time.Duration(float64(step2)*1.2))

	// Backoff grows between attempt 1 and attempt 2 (even with jitter, the
	// windows do not overlap: 6m max vs 12m min).
	assert.True(t, second.After(*first), "retry delay must grow between attempt 1 and attempt 2")
}

// assertRetryWithin checks that a scheduled retry time falls lo..hi from "now",
// tolerating the before/after sampling window around the call.
func assertRetryWithin(t *testing.T, at *time.Time, before, after time.Time, lo, hi time.Duration) {
	t.Helper()
	require.NotNil(t, at)
	assert.False(t, at.Before(before.Add(lo)), "retry %v earlier than %v", at, before.Add(lo))
	assert.False(t, at.After(after.Add(hi)), "retry %v later than %v", at, after.Add(hi))
}

func TestRetryAtNilBeyondMaxAttempts(t *testing.T) {
	// The retry budget is maxDeliveryAttempts (3); retryAt is what stops
	// scheduling once it is spent.
	assert.Nil(t, retryAt(maxDeliveryAttempts, 0))
	assert.Nil(t, retryAt(maxDeliveryAttempts+1, 0))
}

func TestRetryAtHonorsRetryAfterHint(t *testing.T) {
	before := time.Now()
	// A Retry-After far longer than the jittered backoff wins.
	at := retryAt(1, 30*time.Minute)
	require.NotNil(t, at)
	assert.False(t, at.Before(before.Add(30*time.Minute)), "retry-after hint must not be shortened by the backoff")
}

// ---- saveDelivery -----------------------------------------------------

func TestSaveDeliveryPersistsSuccessRecord(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	statusCode := 200

	d := saveDelivery(db, 42, "contact.created", `{"a":1}`, &statusCode, nil, 1, nil, false, "")

	require.NotZero(t, d.ID, "saveDelivery must return the persisted record with its ID populated")

	var loaded models.WebhookDelivery
	require.NoError(t, db.First(&loaded, d.ID).Error)
	assert.EqualValues(t, 42, loaded.WebhookID)
	assert.Equal(t, "contact.created", loaded.EventType)
	assert.Equal(t, `{"a":1}`, loaded.Payload)
	require.NotNil(t, loaded.StatusCode)
	assert.Equal(t, 200, *loaded.StatusCode)
	assert.Nil(t, loaded.Error)
	assert.Equal(t, 1, loaded.Attempts)
	assert.Nil(t, loaded.NextRetryAt)
}

// TestSaveDeliveryLogsAndReturnsOnCreateError covers the db.Create error
// branch: saveDelivery must not panic when persistence fails, and still
// returns the (unsaved) delivery value to its caller.
func TestSaveDeliveryLogsAndReturnsOnCreateError(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	// Drop the table out from under saveDelivery so db.Create fails.
	require.NoError(t, db.Migrator().DropTable(&models.WebhookDelivery{}))

	var d models.WebhookDelivery
	require.NotPanics(t, func() {
		d = saveDelivery(db, 1, "contact.created", `{}`, nil, nil, 1, nil, false, "")
	})
	assert.Zero(t, d.ID, "a failed Create must leave the record unsaved (no ID assigned)")
}

func TestSaveDeliveryPersistsFailureRecordWithRetry(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	errMsg := "unexpected status 500"
	next := time.Now().Add(5 * time.Minute)

	d := saveDelivery(db, 7, "contact.updated", `{}`, nil, &errMsg, 1, &next, false, "")

	var loaded models.WebhookDelivery
	require.NoError(t, db.First(&loaded, d.ID).Error)
	assert.Nil(t, loaded.StatusCode)
	require.NotNil(t, loaded.Error)
	assert.Equal(t, "unexpected status 500", *loaded.Error)
	require.NotNil(t, loaded.NextRetryAt)
	assert.WithinDuration(t, next, *loaded.NextRetryAt, time.Second)
}

// ---- trimSuccessfulDeliveryPayload (issue #622) -----------------------

// The stored receipt for a successful delivery is the event envelope only —
// id, event, timestamp — with the entity body (`data`) removed. The signature
// and the wire body are unaffected (they are built from the full payload
// before this trimming ever runs); only the persisted copy is minimized.
func TestTrimSuccessfulDeliveryPayloadDropsEntityData(t *testing.T) {
	body := []byte(`{"id":"evt_1","event":"contact.created","timestamp":"2026-01-01T00:00:00Z","data":{"fullName":"Alice","email":"alice@example.com","phones":["+15550009999"]}}`)

	trimmed := trimSuccessfulDeliveryPayload(body)

	var env map[string]interface{}
	require.NoError(t, json.Unmarshal(trimmed, &env))
	assert.Equal(t, "evt_1", env["id"])
	assert.Equal(t, "contact.created", env["event"])
	assert.Equal(t, "2026-01-01T00:00:00Z", env["timestamp"])
	assert.NotContains(t, env, "data", "the serialized entity body must not survive in a successful delivery's stored payload")
	assert.NotContains(t, string(trimmed), "alice@example.com", "no contact PII may survive in a successful delivery's stored payload")
	assert.NotContains(t, string(trimmed), "+15550009999")
}

// The envelope is exactly the receipt a consumer needs; keep every envelope
// field so a reader of delivery records sees the same top-level shape it saw
// before, minus the entity.
func TestTrimSuccessfulDeliveryPayloadKeepsEnvelopeShape(t *testing.T) {
	body := []byte(`{"id":"evt_2","event":"contact.updated","timestamp":"2026-02-02T00:00:00Z","data":{}}`)

	trimmed := trimSuccessfulDeliveryPayload(body)

	assert.Equal(t, `{"event":"contact.updated","id":"evt_2","timestamp":"2026-02-02T00:00:00Z"}`, string(trimmed),
		"the trimmed payload must be exactly the three envelope fields (Go marshals maps with sorted keys)")
}

// Fails safe: a body that is not the known envelope shape is kept verbatim —
// trimming must never mangle a receipt it does not understand.
func TestTrimSuccessfulDeliveryPayloadKeepsNonEnvelopeBody(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"not-json", []byte(`not json`)},
		{"no-data-key", []byte(`{"id":"x","event":"e"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.body, trimSuccessfulDeliveryPayload(tc.body))
		})
	}
}

// A successful delivery through the real HTTP path persists the trimmed
// payload while still sending the full body (and its signature) over the wire.
func TestDeliverWebhookSuccessPersistsTrimmedPayload(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = b
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh := newTestWebhook(server.URL, "secret")
	require.NoError(t, db.Create(&wh).Error)

	body := []byte(`{"id":"evt_3","event":"contact.created","timestamp":"2026-03-03T00:00:00Z","data":{"fullName":"Ada","email":"ada@example.com"}}`)
	delivery := deliverWebhook(context.Background(), db, config.Config{}, wh, "contact.created", body, 1)

	require.NotNil(t, delivery.StatusCode)
	assert.Equal(t, 200, *delivery.StatusCode)

	// The wire body is the full payload (the receiver still gets the entity)...
	assert.Equal(t, body, receivedBody, "the HTTP request must carry the full entity body")
	// ...but the persisted copy is the envelope only.
	var loaded models.WebhookDelivery
	require.NoError(t, db.First(&loaded, delivery.ID).Error)
	assert.NotContains(t, loaded.Payload, "ada@example.com", "the stored delivery must not keep the entity body")
	assert.NotContains(t, loaded.Payload, "data")
	assert.Contains(t, loaded.Payload, `"event":"contact.created"`)
}

// A failed delivery keeps the full payload: ProcessWebhookRetries replays it
// verbatim on the next attempt, so trimming there would break re-delivery.
func TestDeliverWebhookFailureKeepsFullPayload(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	wh := newTestWebhook(server.URL, "secret")
	require.NoError(t, db.Create(&wh).Error)

	body := []byte(`{"id":"evt_4","event":"contact.created","timestamp":"2026-04-04T00:00:00Z","data":{"fullName":"Bob","email":"bob@example.com"}}`)
	delivery := deliverWebhook(context.Background(), db, config.Config{}, wh, "contact.created", body, 1)

	require.NotNil(t, delivery.StatusCode)
	assert.Equal(t, 500, *delivery.StatusCode)

	var loaded models.WebhookDelivery
	require.NoError(t, db.First(&loaded, delivery.ID).Error)
	assert.Equal(t, string(body), loaded.Payload, "a failed delivery must retain the full body for re-send")
	require.NotNil(t, loaded.NextRetryAt, "the failed delivery must still be scheduled for retry")
}

// ---- deliverWebhook ---------------------------------------------------

func newTestWebhook(url, secret string) models.Webhook {
	return models.Webhook{
		UserID:   1,
		Name:     "test hook",
		URL:      url,
		Events:   []string{"contact.created"},
		Secret:   secret,
		IsActive: true,
	}
}

// TestDeliverWebhookSendsValidSignature is the primary "actual HTTP delivery"
// test: it stands up a real HTTP server, delivers through deliverWebhook, and
// asserts the request the server actually received carries a correct,
// independently-verifiable signature, the right headers, and the right body
// -- not just that deliverWebhook returned without error.
func TestDeliverWebhookSendsValidSignature(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	secret := "whsec_abc123"

	var (
		receivedBody      []byte
		receivedSignature string
		receivedEventHdr  string
		receivedContentTy string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = b
		receivedSignature = r.Header.Get("X-Webhook-Signature")
		receivedEventHdr = r.Header.Get("X-Mycorrhizal-Event")
		receivedContentTy = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh := newTestWebhook(server.URL, secret)
	require.NoError(t, db.Create(&wh).Error)

	body := []byte(`{"id":"evt_1","event":"contact.created","data":{"name":"Ada"}}`)
	cfg := config.Config{WebhookBlockPrivateURLs: false}

	delivery := deliverWebhook(context.Background(), db, cfg, wh, "contact.created", body, 1)

	require.NotNil(t, delivery.StatusCode)
	assert.Equal(t, 200, *delivery.StatusCode)
	assert.Nil(t, delivery.Error)
	assert.Nil(t, delivery.NextRetryAt, "a successful delivery must not schedule a retry")

	// The server must have received exactly the signed body...
	assert.Equal(t, body, receivedBody)
	assert.Equal(t, "application/json", receivedContentTy)
	assert.Equal(t, "contact.created", receivedEventHdr)

	// ...and the signature header must be independently verifiable by a
	// receiver who only knows the shared secret and the raw body.
	require.True(t, len(receivedSignature) > len("sha256="), "signature header must be present")
	require.Equal(t, "sha256=", receivedSignature[:len("sha256=")])
	gotHex := receivedSignature[len("sha256="):]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	wantHex := fmt.Sprintf("%x", mac.Sum(nil))
	assert.Equal(t, wantHex, gotHex, "receiver-side HMAC verification of the delivered request must succeed")
}

func TestDeliverWebhookNon2xxSchedulesRetryAndRecordsStatus(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	wh := newTestWebhook(server.URL, "secret")
	require.NoError(t, db.Create(&wh).Error)
	cfg := config.Config{}

	before := time.Now()
	delivery := deliverWebhook(context.Background(), db, cfg, wh, "contact.created", []byte(`{}`), 1)
	after := time.Now()

	require.NotNil(t, delivery.StatusCode)
	assert.Equal(t, 500, *delivery.StatusCode)
	require.NotNil(t, delivery.Error)
	assert.Equal(t, "unexpected status 500", *delivery.Error)
	require.NotNil(t, delivery.NextRetryAt, "a failed delivery on attempt 1 (500 is transient) must be scheduled for retry")
	// webhookRetryPolicy: attempt 1 waits ~BaseDelay (5m) ± 20% jitter.
	base := webhookRetryPolicy.BaseDelay
	assertRetryWithin(t, delivery.NextRetryAt, before, after,
		time.Duration(float64(base)*0.8), time.Duration(float64(base)*1.2))
	assert.Equal(t, 1, delivery.Attempts)
	assert.False(t, delivery.FailedPermanently, "a 500 is transient, not a permanent failure")
}

func TestDeliverWebhookFinalAttemptDoesNotScheduleRetry(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	wh := newTestWebhook(server.URL, "secret")
	require.NoError(t, db.Create(&wh).Error)
	cfg := config.Config{}

	// attempt 3 spends the retry budget, so no further retry is scheduled
	// even though the request itself still fails.
	delivery := deliverWebhook(context.Background(), db, cfg, wh, "contact.created", []byte(`{}`), maxDeliveryAttempts)

	require.NotNil(t, delivery.StatusCode)
	assert.Equal(t, 500, *delivery.StatusCode)
	assert.Nil(t, delivery.NextRetryAt, "exhausted retry budget must not schedule another retry")
	assert.Equal(t, maxDeliveryAttempts, delivery.Attempts)

	// The exhausted, still-failing delivery emits one integration_failed
	// operational event (issue #424) — the point an operator needs to know an
	// external integration is down. Detail carries the event type, never the URL.
	var ev models.SystemEvent
	require.NoError(t, db.Where("event_type = ?", models.SysEventIntegrationFailed).First(&ev).Error)
	assert.Equal(t, logger.ComponentWebhook, ev.Component)
	assert.Equal(t, logger.SeverityError, ev.Severity)
	assert.Contains(t, ev.Detail, "event=contact.created")
	assert.NotContains(t, ev.Detail, server.URL)
}

func TestDeliverWebhookMidRetryDoesNotEmitIntegrationFailed(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	wh := newTestWebhook(server.URL, "secret")
	require.NoError(t, db.Create(&wh).Error)

	// attempt 1 of 3 — a retry is still scheduled, so this is not yet a
	// "the integration is down" event.
	deliverWebhook(context.Background(), db, config.Config{}, wh, "contact.created", []byte(`{}`), 1)

	var count int64
	require.NoError(t, db.Model(&models.SystemEvent{}).
		Where("event_type = ?", models.SysEventIntegrationFailed).Count(&count).Error)
	assert.Zero(t, count, "a mid-retry failure must not emit integration_failed")
}

func TestDeliverWebhookSetsCorrelationHeader(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	var gotCorr string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCorr = r.Header.Get("X-Correlation-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh := newTestWebhook(server.URL, "secret")
	require.NoError(t, db.Create(&wh).Error)

	ctx := logger.WithCorrelationID(context.Background(), "req-abc-123")
	deliverWebhook(ctx, db, config.Config{}, wh, "contact.created", []byte(`{}`), 1)

	assert.Equal(t, "req-abc-123", gotCorr)
}

func TestDeliverWebhookConnectionErrorSchedulesRetry(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	// Port 1 on loopback: nothing listens there, so the client.Do call fails
	// with a connection error rather than returning a response.
	wh := newTestWebhook("http://127.0.0.1:1/hook", "secret")
	require.NoError(t, db.Create(&wh).Error)
	cfg := config.Config{WebhookBlockPrivateURLs: false}

	delivery := deliverWebhook(context.Background(), db, cfg, wh, "contact.created", []byte(`{}`), 1)

	assert.Nil(t, delivery.StatusCode, "a transport-level failure never gets a status code")
	require.NotNil(t, delivery.Error)
	assert.Equal(t, genericDeliveryTransportError, *delivery.Error,
		"the stored transport error must be generic, not the raw dial error (issue #869)")
	require.NotNil(t, delivery.NextRetryAt)
}

func TestDeliverWebhookMalformedURLSchedulesRetry(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	// Malformed enough that http.NewRequest itself fails (invalid IPv6
	// literal, unbalanced bracket) while cfg.WebhookBlockPrivateURLs is off so
	// the isPrivateURL pre-check is skipped and this exercises the
	// http.NewRequest error branch specifically.
	wh := newTestWebhook("http://[::1", "secret")
	require.NoError(t, db.Create(&wh).Error)
	cfg := config.Config{WebhookBlockPrivateURLs: false}

	delivery := deliverWebhook(context.Background(), db, cfg, wh, "contact.created", []byte(`{}`), 1)

	assert.Nil(t, delivery.StatusCode)
	require.NotNil(t, delivery.Error)
	assert.Equal(t, genericDeliveryInvalidURL, *delivery.Error,
		"the http.NewRequest error branch must store a generic string, not the raw URL parse error (issue #869)")
	require.NotNil(t, delivery.NextRetryAt)
}

func TestDeliverWebhookBlocksPrivateURLWhenConfigured(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// httptest servers listen on 127.0.0.1, a loopback address, so with the
	// guard enabled this must be rejected before any request is sent.
	wh := newTestWebhook(server.URL, "secret")
	require.NoError(t, db.Create(&wh).Error)
	cfg := config.Config{WebhookBlockPrivateURLs: true}

	delivery := deliverWebhook(context.Background(), db, cfg, wh, "contact.created", []byte(`{}`), 1)

	assert.Equal(t, int32(0), atomic.LoadInt32(&hits), "the guarded delivery must never reach the destination server")
	assert.Nil(t, delivery.StatusCode)
	require.NotNil(t, delivery.Error)
	assert.Equal(t, "webhook URL resolves to a private or loopback address", *delivery.Error)
	// This is the pre-flight branch in deliverWebhook (distinct from the
	// dialer-level guard); it does not call retryAt at all, so no retry is
	// scheduled here even though it's attempt 1.
	assert.Nil(t, delivery.NextRetryAt)
}

// ---- TriggerWebhooks ----------------------------------------------------

func TestTriggerWebhooksFansOutToSubscribedActiveWebhooksOnly(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const userID = uint(1)

	subscribedActive := models.Webhook{UserID: userID, Name: "match", URL: server.URL, Events: []string{"contact.created"}, Secret: "s1", IsActive: true}
	require.NoError(t, db.Create(&subscribedActive).Error)

	notSubscribed := models.Webhook{UserID: userID, Name: "no-match", URL: server.URL, Events: []string{"contact.updated"}, Secret: "s2", IsActive: true}
	require.NoError(t, db.Create(&notSubscribed).Error)

	inactive := models.Webhook{UserID: userID, Name: "inactive", URL: server.URL, Events: []string{"contact.created"}, Secret: "s3", IsActive: false}
	require.NoError(t, db.Create(&inactive).Error)
	// models.Webhook.IsActive carries `gorm:"default:true"`; GORM treats the Go
	// zero value (false) on Create as "unset" and silently applies the SQL
	// default instead, so an explicit UpdateColumn is needed to actually
	// persist IsActive=false here.
	require.NoError(t, db.Model(&inactive).UpdateColumn("is_active", false).Error)

	otherUser := models.Webhook{UserID: userID + 1, Name: "other-user", URL: server.URL, Events: []string{"contact.created"}, Secret: "s4", IsActive: true}
	require.NoError(t, db.Create(&otherUser).Error)

	softDeleted := models.Webhook{UserID: userID, Name: "deleted", URL: server.URL, Events: []string{"contact.created"}, Secret: "s5", IsActive: true}
	require.NoError(t, db.Create(&softDeleted).Error)
	require.NoError(t, db.Delete(&softDeleted).Error)

	cfg := config.Config{}
	TriggerWebhooks(context.Background(), db, cfg, userID, "contact.created", map[string]string{"name": "Ada"})

	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&hits) >= 1
	}, 3*time.Second, 10*time.Millisecond, "the subscribed active webhook must be delivered to")

	// Give any accidental extra deliveries a moment to land, then confirm
	// exactly one delivery was made and it's attributed to the right webhook.
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits), "only the subscribed, active, non-deleted webhook for this user must fire")

	var deliveries []models.WebhookDelivery
	require.NoError(t, db.Find(&deliveries).Error)
	require.Len(t, deliveries, 1)
	assert.Equal(t, subscribedActive.ID, deliveries[0].WebhookID)
	assert.Equal(t, "contact.created", deliveries[0].EventType)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(deliveries[0].Payload), &payload))
	assert.Equal(t, "contact.created", payload["event"])
}

func TestTriggerWebhooksNoActiveSubscriptionsDeliversNothing(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	cfg := config.Config{}

	assert.NotPanics(t, func() {
		TriggerWebhooks(context.Background(), db, cfg, 999, "contact.created", map[string]string{"x": "y"})
	})

	time.Sleep(50 * time.Millisecond)
	var deliveries []models.WebhookDelivery
	require.NoError(t, db.Find(&deliveries).Error)
	assert.Empty(t, deliveries)
}

// TestTriggerWebhooksDBQueryErrorIsNoop covers the webhooks-lookup error
// branch: if the initial query fails, TriggerWebhooks must log and return
// without attempting any delivery, rather than panicking on a nil/empty slice.
func TestTriggerWebhooksDBQueryErrorIsNoop(t *testing.T) {
	db := setupWebhookRetryTestDB(t)
	// Drop the table out from under the lookup query so it errors.
	require.NoError(t, db.Migrator().DropTable(&models.Webhook{}))

	assert.NotPanics(t, func() {
		TriggerWebhooks(context.Background(), db, config.Config{}, 1, "contact.created", map[string]string{"x": "y"})
	})
}

// TestTriggerWebhooksPayloadMarshalFailureIsNoop covers the buildPayloadBody
// error branch: TriggerWebhooks must log and return without dispatching to
// any webhook rather than panicking or sending a broken payload.
func TestTriggerWebhooksPayloadMarshalFailureIsNoop(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const userID = uint(1)
	wh := models.Webhook{UserID: userID, Name: "match", URL: server.URL, Events: []string{"contact.created"}, Secret: "s1", IsActive: true}
	require.NoError(t, db.Create(&wh).Error)

	// channels are not JSON-marshalable, so buildPayloadBody must fail here.
	unmarshalable := make(chan int)

	assert.NotPanics(t, func() {
		TriggerWebhooks(context.Background(), db, config.Config{}, userID, "contact.created", unmarshalable)
	})

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&hits), "a payload that fails to marshal must never reach a webhook")

	var deliveries []models.WebhookDelivery
	require.NoError(t, db.Find(&deliveries).Error)
	assert.Empty(t, deliveries)
}

// ---- TestWebhookDelivery ---------------------------------------------------

func TestTestWebhookDeliverySendsTestPayloadAndPersists(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	var receivedEventHdr string
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedBody = b
		receivedEventHdr = r.Header.Get("X-Mycorrhizal-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wh := models.Webhook{UserID: 1, Name: "test", URL: server.URL, Events: []string{"contact.created"}, Secret: "s1", IsActive: true}
	require.NoError(t, db.Create(&wh).Error)

	delivery := TestWebhookDelivery(db, config.Config{}, wh)

	require.NotNil(t, delivery.StatusCode)
	assert.Equal(t, 200, *delivery.StatusCode)
	assert.Nil(t, delivery.Error)
	assert.Equal(t, "test", delivery.EventType)
	assert.Equal(t, "test", receivedEventHdr)
	// Ignores event subscriptions entirely: this webhook is only subscribed to
	// contact.created, yet the test delivery still goes out.

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(receivedBody, &payload))
	assert.Equal(t, "test", payload["event"])
	data, ok := payload["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Contains(t, data["message"], "test webhook delivery")

	// Persisted, findable by ID.
	var loaded models.WebhookDelivery
	require.NoError(t, db.First(&loaded, delivery.ID).Error)
	assert.Equal(t, delivery.WebhookID, loaded.WebhookID)
}

func TestTestWebhookDeliveryRecordsFailureOnUnreachableURL(t *testing.T) {
	db := setupWebhookRetryTestDB(t)

	wh := models.Webhook{UserID: 1, Name: "test", URL: "http://127.0.0.1:1/hook", Events: []string{}, Secret: "s1", IsActive: true}
	require.NoError(t, db.Create(&wh).Error)

	delivery := TestWebhookDelivery(db, config.Config{}, wh)

	assert.Nil(t, delivery.StatusCode)
	require.NotNil(t, delivery.Error)
	// The /test endpoint echoes this string straight back to the caller, so it
	// must not name the dialed host:port or say "connection refused" (#869).
	assert.Equal(t, genericDeliveryTransportError, *delivery.Error)
	assert.NotContains(t, *delivery.Error, "127.0.0.1")
	assert.Equal(t, "test", delivery.EventType)
}
