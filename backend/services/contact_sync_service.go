package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mycorrhizal/config"
	"mycorrhizal/contactmodel"
	"mycorrhizal/httputil"
	"mycorrhizal/internal/faults"
	"mycorrhizal/logger"
	"mycorrhizal/models"
	"mycorrhizal/vcard3"
	"mycorrhizal/vcard4"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-vcard"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
	"gorm.io/gorm"
)

// Sentinel errors for contact (CardDAV client) sync failures, mapped to API
// errors in the controller. Named/shaped like calendar_sync_service.go's own
// sentinels (ErrCalendar*), kept as a separate set rather than shared: a
// contacts-specific message ("contact subscription ...") is clearer to API
// consumers than a reused calendar-flavored one, and it avoids coupling the
// two sync paths' error semantics together.
var (
	ErrContactSyncInvalidURL     = errors.New("contact subscription URL is invalid")
	ErrContactSyncUnauthorized   = errors.New("contact subscription authentication failed")
	ErrContactSyncNotFound       = errors.New("contact subscription address book not found")
	ErrContactSyncUnreachable    = errors.New("contact subscription server could not be reached")
	ErrContactSyncInvalidData    = errors.New("contact subscription server returned data that could not be parsed")
	ErrContactSyncTooLarge       = errors.New("contact subscription response exceeds the size limit")
	ErrContactSyncPrivateAddress = errors.New("contact subscription URL resolves to a private or loopback address")
)

const (
	maxContactResponseBytes   = 20 * 1024 * 1024
	maxContactsPerSync        = 2000
	contactSyncRequestTimeout = 60 * time.Second
)

// ContactSyncStats summarizes one sync run of a subscription.
type ContactSyncStats struct {
	Created  int `json:"created"`
	Updated  int `json:"updated"`
	Archived int `json:"archived"`
	Skipped  int `json:"skipped"`
}

// ContactSyncService pulls contacts in from an external CardDAV address
// book ( — the opposite direction of the CardDAV *server*).
//
// Architecture mirrors CalendarSyncService (services/calendar_sync_service.go)
// exactly: subscription config, per-subscription sync-mutex, encrypted
// credentials, link-table for idempotent re-sync, content-hash change
// detection, sync status/error bookkeeping. redactURLPassword/redactedError/
// clampRunes/limitedBody/privateBlockingDialContext-shaped helpers are
// reused where they're domain-agnostic (see below for what's reused vs.
// duplicated, and why).
type ContactSyncService struct {
	client *http.Client
}

// NewContactSyncService builds a sync service. When blockPrivateURLs is set,
// connections to private/loopback/link-local addresses are refused (SSRF
// protection for cloud deployments) — same rationale and opt-in knob as
// CalendarSyncService, reusing config.Config.CalDAVBlockPrivateURLs (no new
// config field: config/ is outside this WP's file scope, and "block sync
// requests to external servers from resolving to internal addresses" is the
// same security concern for both CalDAV and CardDAV client sync).
func NewContactSyncService(blockPrivateURLs bool) *ContactSyncService {
	transport := &http.Transport{}
	if blockPrivateURLs {
		transport.DialContext = contactPrivateBlockingDialContext
	}
	return &ContactSyncService{
		client: &http.Client{
			Timeout:   contactSyncRequestTimeout,
			Transport: &contactRoundTripper{base: transport},
		},
	}
}

// contactPrivateBlockingDialContext mirrors calendar_sync_service.go's
// privateBlockingDialContext, returning the contacts-specific sentinel errors.
// The address classification and IP-pinning are shared (httputil.SafeDialContext)
// so every outbound path enforces the same reserved ranges; only the sentinels
// differ, which is what kept these two apart in the first place.
func contactPrivateBlockingDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dial := httputil.SafeDialContext(
		fmt.Errorf("%w: could not resolve host", ErrContactSyncUnreachable),
		ErrContactSyncPrivateAddress,
	)
	return dial(ctx, network, addr)
}

// contactRoundTripper maps HTTP auth/not-found responses to sentinel errors
// and caps the response body size. Mirrors calendar_sync_service.go's
// calendarRoundTripper; not shared for the same reason as the dial context
// above (different sentinel set).
type contactRoundTripper struct {
	base http.RoundTripper
}

func (t *contactRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Issue #434 failure-injection seam: an armed fault replaces the round
	// trip, exactly as an unreachable / auth-expired / deleted-remotely
	// upstream presents to reconcileContactSync and its post-run bookkeeping.
	if err := faults.Hook(faultContactSyncRequest); err != nil {
		return nil, err
	}
	// Carry the caller's correlation ID onto the outbound request so the
	// remote's access log (and ours, on the response) can be tied back to the
	// UI action or scheduled run that triggered the sync (issue #425).
	if id := logger.CorrelationID(req.Context()); id != "" {
		req.Header.Set("X-Correlation-ID", id)
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		resp.Body.Close()
		return nil, ErrContactSyncUnauthorized
	case http.StatusNotFound, http.StatusGone:
		resp.Body.Close()
		return nil, ErrContactSyncNotFound
	}
	resp.Body = &contactLimitedBody{inner: resp.Body, remaining: maxContactResponseBytes}
	return resp, nil
}

// contactLimitedBody mirrors calendar_sync_service.go's limitedBody, with
// the contacts-specific "too large" sentinel.
type contactLimitedBody struct {
	inner     io.ReadCloser
	remaining int64
}

func (b *contactLimitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		var probe [1]byte
		n, err := b.inner.Read(probe[:])
		if n > 0 {
			return 0, ErrContactSyncTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.inner.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *contactLimitedBody) Close() error { return b.inner.Close() }

// NormalizeContactSubscriptionURL validates a subscription URL. v1 treats
// the URL as a direct address-book collection URL (see syncSubscription's
// doc comment for why discovery is skipped in v1).
func NormalizeContactSubscriptionURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, ErrContactSyncInvalidURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, ErrContactSyncInvalidURL
	}
	if parsed.Hostname() == "" {
		return nil, ErrContactSyncInvalidURL
	}
	return parsed, nil
}

var contactSubscriptionLocks sync.Map // subscription ID -> *sync.Mutex

// SyncSubscription syncs contacts in from the subscription's CardDAV address
// book and updates its last-sync bookkeeping fields in the database.
// Mirrors CalendarSyncService.SyncSubscription's exact bookkeeping shape.
func (s *ContactSyncService) SyncSubscription(ctx context.Context, db *gorm.DB, cfg config.Config, sub *models.ContactSubscription) (ContactSyncStats, error) {
	lock, _ := contactSubscriptionLocks.LoadOrStore(sub.ID, &sync.Mutex{})
	mutex := lock.(*sync.Mutex)
	mutex.Lock()
	defer mutex.Unlock()

	start := time.Now()
	stats, newToken, err := s.syncSubscription(ctx, db, cfg, sub)
	runDuration := time.Since(start)
	err = redactURLPassword(err, sub.URL)

	recordSyncEvent(ctx, db, logger.ComponentContactSync, sub.UserID, start, err,
		fmt.Sprintf("created=%d updated=%d archived=%d skipped=%d",
			stats.Created, stats.Updated, stats.Archived, stats.Skipped))

	now := time.Now().UTC()
	sub.LastSyncedAt = &now
	updates := map[string]interface{}{
		"last_synced_at": sub.LastSyncedAt,
	}
	if err != nil {
		sub.LastSyncStatus = models.ContactSyncStatusError
		sub.LastSyncError = clampRunes(err.Error(), 500)
	} else {
		sub.LastSyncStatus = models.ContactSyncStatusSuccess
		sub.LastSyncError = ""
		sub.SyncToken = newToken
		updates["sync_token"] = sub.SyncToken
	}
	updates["last_sync_status"] = sub.LastSyncStatus
	updates["last_sync_error"] = sub.LastSyncError

	// Sync-health last-known-good state (issue #390): fold this run into the
	// consecutive-failure / incident / last-success bookkeeping. A permanent
	// failure also enters the terminal state (INT-04, #467) so the UI can
	// surface "sync stopped — action needed" instead of a silent stale book.
	for k, v := range sub.AdvanceForRun(now, runDuration, marshalSyncRunStats(stats), err, classifySyncFailure(err)) {
		updates[k] = v
	}

	if saveErr := db.Model(&models.ContactSubscription{}).Where("id = ?", sub.ID).Updates(updates).Error; saveErr != nil {
		logger.Error().Err(saveErr).Uint("subscription_id", sub.ID).Msg("Failed to update contact sync status")
	}

	return stats, err
}

// syncSubscription is SyncSubscription's actual logic, run under the
// per-subscription lock.
//
// v1 treats sub.URL as a direct address-book collection URL, skipping
// FindCurrentUserPrincipal/FindAddressBookHomeSet/FindAddressBooks discovery
// entirely (docs/adrs/0001-neutral-hub-and-spoke-contact-model.md accepted
// v1 simplification) — full discovery, and syncing more than one address
// book per subscription, are both documented fast-follows, not implemented
// here.
func (s *ContactSyncService) syncSubscription(ctx context.Context, db *gorm.DB, cfg config.Config, sub *models.ContactSubscription) (ContactSyncStats, string, error) {
	parsedURL, err := NormalizeContactSubscriptionURL(sub.URL)
	if err != nil {
		return ContactSyncStats{}, sub.SyncToken, err
	}

	password, err := DecryptCredential(cfg.JWTSecretKey, sub.PasswordEncrypted)
	if err != nil {
		return ContactSyncStats{}, sub.SyncToken, fmt.Errorf("%w: %v", ErrContactSyncUnauthorized, err)
	}

	var httpClient webdav.HTTPClient = s.client
	if sub.Username != "" || password != "" {
		httpClient = webdav.HTTPClientWithBasicAuth(s.client, sub.Username, password)
	}

	client, err := carddav.NewClient(httpClient, parsedURL.String())
	if err != nil {
		return ContactSyncStats{}, sub.SyncToken, fmt.Errorf("%w: %v", ErrContactSyncInvalidURL, err)
	}

	// Capture the pre-fetch state of any linked contact whose stored conflict
	// baseline is missing/corrupt, *before* the network fetch below. This is
	// the reference that lets reconcile see a REST edit landing during the
	// fetch as a local edit instead of silently full-replacing it (#919).
	preSyncBaselines, err := capturePreSyncBaselines(db, sub)
	if err != nil {
		return ContactSyncStats{}, sub.SyncToken, err
	}

	updated, deleted, newToken, fullRefetch, err := s.fetchChanges(ctx, client, parsedURL, sub.SyncToken)
	if err != nil {
		return ContactSyncStats{}, sub.SyncToken, err
	}

	if len(updated) > maxContactsPerSync {
		logger.Warn().Uint("subscription_id", sub.ID).Int("objects", len(updated)).Int("limit", maxContactsPerSync).
			Msg("Contact subscription returned more objects than the per-sync limit, truncating")
		updated = updated[:maxContactsPerSync]
	}

	stats, err := reconcileContactSyncWithBaselines(db, sub, updated, deleted, fullRefetch, cfg.ProfilePhotoDir, preSyncBaselines)
	if err != nil {
		return ContactSyncStats{}, sub.SyncToken, err
	}

	return stats, newToken, nil
}

// fetchChanges retrieves changed/deleted address objects, preferring RFC
// 6578 sync-collection (delta sync) and falling back to a full
// addressbook-query refetch for servers that don't support it.
//
// **Library gotcha, confirmed by reading go-webdav v0.7.0's source
// directly** (carddav/client.go's SyncCollection): even though SyncQuery
// accepts a DataRequest, the Updated AddressObjects it returns carry only
// Path/ModTime/ETag — Card is left zero-valued (SyncCollection's response
// loop never calls the address-data decoder that QueryAddressBook/
// MultiGetAddressBook use). So sync-collection alone only tells us *which*
// resources changed, not their content. This function follows the standard
// two-step CardDAV sync-collection pattern to compensate: sync-collection
// for the changed/deleted path list, then addressbook-multiget for the
// bodies of the changed ones. (This is also why AllProp is requested in the
// sync-collection query below even though it's not actually honored by this
// client version for Updated entries — harmless, and forward-compatible if
// a future go-webdav version starts honoring it.)
//
// fullRefetch is true when sync-collection wasn't usable (any error is
// treated as "unsupported",  own note that go-webdav exposes no
// typed "sync-collection unsupported" error) and a full QueryAddressBook was
// used instead. QueryAddressBook returns the address book's *entire* current
// membership, not a delta — reconcileContactSync uses fullRefetch to know it
// must diff that membership against existing links itself to find
// deletions, since the server gives no explicit Deleted list in this mode.
func (s *ContactSyncService) fetchChanges(ctx context.Context, client *carddav.Client, addrBookURL *url.URL, syncToken string) (updated []carddav.AddressObject, deleted []string, newToken string, fullRefetch bool, err error) {
	resp, syncErr := client.SyncCollection(ctx, addrBookURL.Path, &carddav.SyncQuery{
		DataRequest: carddav.AddressDataRequest{AllProp: true},
		SyncToken:   syncToken,
	})
	if syncErr == nil {
		// RFC 6578 §3.2 requires every sync-collection response to carry a
		// sync-token. A server that answers the REPORT but omits it (or
		// returns an empty element) would otherwise silently poison the next
		// run: SyncSubscription stores the empty token, the following run
		// sends it back, and the server treats it as "first sync" — a full
		// refetch of the whole book on every run, forever, with no visible
		// error (issue #496 item 2, "silent full resync"). Degrade to a
		// defined behavior instead: process the book as a full refetch for
		// this run (reconcileContactSync diffs membership to find deletions)
		// and keep the previous token, so a healthy token-receiving server
		// never regresses into the full-resync loop.
		if resp.SyncToken == "" {
			logger.Warn().Str("url", addrBookURL.Redacted()).
				Msg("CardDAV sync-collection response carried no sync-token (RFC 6578 violation); degrading to a full refetch and keeping the previous token")
			full, qErr := client.QueryAddressBook(ctx, addrBookURL.Path, &carddav.AddressBookQuery{
				DataRequest: carddav.AddressDataRequest{AllProp: true},
			})
			if qErr != nil {
				return nil, nil, "", false, classifyContactSyncError(qErr)
			}
			return full, nil, syncToken, true, nil
		}
		if len(resp.Updated) == 0 {
			return nil, resp.Deleted, resp.SyncToken, false, nil
		}

		paths := make([]string, len(resp.Updated))
		for i, obj := range resp.Updated {
			paths[i] = obj.Path
		}
		full, mgErr := client.MultiGetAddressBook(ctx, addrBookURL.Path, &carddav.AddressBookMultiGet{
			Paths:       paths,
			DataRequest: carddav.AddressDataRequest{AllProp: true},
		})
		if mgErr != nil {
			return nil, nil, "", false, classifyContactSyncError(mgErr)
		}
		return full, resp.Deleted, resp.SyncToken, false, nil
	}

	if isContactSyncSentinel(syncErr) {
		// A real auth/not-found/too-large/private-address failure: falling
		// back to a full refetch would just hit the same wall again, so
		// propagate it as a hard failure instead (mirrors
		// calendar_sync_service.go's fetchEvents: same sentinel-check-then-
		// propagate-or-fallback shape).
		return nil, nil, "", false, syncErr
	}

	logger.Debug().Err(syncErr).Str("url", addrBookURL.Redacted()).
		Msg("CardDAV sync-collection failed, falling back to full addressbook-query refetch")

	full, qErr := client.QueryAddressBook(ctx, addrBookURL.Path, &carddav.AddressBookQuery{
		DataRequest: carddav.AddressDataRequest{AllProp: true},
	})
	if qErr != nil {
		return nil, nil, "", false, classifyContactSyncError(qErr)
	}
	return full, nil, "", true, nil
}

func isContactSyncSentinel(err error) bool {
	return errors.Is(err, ErrContactSyncUnauthorized) || errors.Is(err, ErrContactSyncNotFound) ||
		errors.Is(err, ErrContactSyncTooLarge) || errors.Is(err, ErrContactSyncPrivateAddress)
}

func classifyContactSyncError(err error) error {
	if isContactSyncSentinel(err) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrContactSyncUnreachable, err)
}

// reconcileContactSync applies a batch of changed/deleted CardDAV address
// objects to the local database: creating or updating real Contact rows
// (via models.ApplyRecordToContact, shared Record->Contact mapping
// — no third mapping is written here) and archiving Contacts whose remote
// object was deleted. Kept as a standalone function (not a method, and
// taking already-fetched data rather than a live *carddav.Client) so it can
// be unit-tested directly against constructed AddressObject/path values
// without spinning up an HTTP server.
//
// It is the no-pre-sync-baselines entry point: links whose stored conflict
// baseline is missing/corrupt get no conflict detection for that run (the
// historical "no baseline, no conflicts" behavior). SyncSubscription uses
// reconcileContactSyncWithBaselines instead, with the pre-fetch snapshots
// capturePreSyncBaselines took (#919).
func reconcileContactSync(db *gorm.DB, sub *models.ContactSubscription, updated []carddav.AddressObject, deletedPaths []string, fullRefetch bool, photoDir string) (ContactSyncStats, error) {
	return reconcileContactSyncWithBaselines(db, sub, updated, deletedPaths, fullRefetch, photoDir, nil)
}

// reconcileContactSyncWithBaselines is reconcileContactSync plus the
// per-contact pre-sync baselines (keyed by contact ID) capturePreSyncBaselines
// took before the network fetch. On the update path, when a link's own stored
// baseline is missing/corrupt, the pre-sync snapshot is used as the effective
// baseline so a concurrent local edit is recorded rather than dropped.
func reconcileContactSyncWithBaselines(db *gorm.DB, sub *models.ContactSubscription, updated []carddav.AddressObject, deletedPaths []string, fullRefetch bool, photoDir string, preSyncBaselines map[uint]map[string]string) (ContactSyncStats, error) {
	var stats ContactSyncStats

	err := db.Transaction(func(tx *gorm.DB) error {
		seenPaths := make(map[string]struct{}, len(updated))

		for _, obj := range updated {
			seenPaths[obj.Path] = struct{}{}

			var buf bytes.Buffer
			if err := vcard.NewEncoder(&buf).Encode(obj.Card); err != nil {
				logger.Warn().Err(err).Str("path", obj.Path).Msg("Failed to re-encode synced vCard, skipping")
				stats.Skipped++
				continue
			}
			raw := buf.Bytes()

			var adapter contactmodel.Importer = vcard4.Adapter{}
			if sniffVCardVersion(raw) == "3.0" {
				adapter = vcard3.Adapter{}
			}
			record, diags, importErr := adapter.Import(raw)
			if importErr != nil {
				logger.Warn().Err(importErr).Str("path", obj.Path).Msg("Failed to parse synced vCard, skipping")
				stats.Skipped++
				continue
			}
			for _, d := range diags {
				logger.Debug().Str("path", obj.Path).Str("severity", d.Severity).Str("concept", d.Concept).Msg(d.Message)
			}

			hash := contactContentHash(raw)

			var link models.ContactSyncLink
			linkErr := tx.Where("subscription_id = ? AND href = ?", sub.ID, obj.Path).First(&link).Error
			switch {
			case errors.Is(linkErr, gorm.ErrRecordNotFound):
				contact := models.Contact{UserID: sub.UserID}
				models.ApplyRecordToContact(&contact, record, photoDir)
				// Issue #512: a CardDAV server is a second, untrusted
				// ingestion point for the exact same hostile bytes the
				// VCF/CSV import assistant already guards against (issue
				// #416's SanitizeImportedContact) -- control characters and
				// invalid UTF-8 in a synced vCard's free-text fields must be
				// cleaned here too, not just on the import-assistant path.
				SanitizeImportedContact(&contact)
				syncedValues := syncConflictSnapshotJSON(syncConflictFieldSnapshot(&contact))
				if err := tx.Create(&contact).Error; err != nil {
					return err
				}
				if err := tx.Create(&models.ContactSyncLink{
					SubscriptionID: sub.ID,
					UserID:         sub.UserID,
					Href:           obj.Path,
					ContactID:      contact.ID,
					ETag:           obj.ETag,
					ContentHash:    hash,
					SyncedValues:   syncedValues,
				}).Error; err != nil {
					return err
				}
				stats.Created++

			case linkErr != nil:
				return linkErr

			default:
				if link.ContentHash == hash {
					stats.Skipped++
					continue
				}

				var contact models.Contact
				cErr := tx.Where("id = ? AND user_id = ?", link.ContactID, sub.UserID).First(&contact).Error
				if errors.Is(cErr, gorm.ErrRecordNotFound) {
					// The user deleted the previously-synced contact;
					// respect that and drop the stale link instead of
					// recreating it (mirrors calendar_sync_service.go's
					// importEvents: "the user deleted the imported
					// activity; respect that").
					if err := tx.Delete(&link).Error; err != nil {
						return err
					}
					stats.Skipped++
					continue
				}
				if cErr != nil {
					return cErr
				}

				// Full-replace, not a field-level merge: ApplyRecordToContact
				// repopulates every flat field from the incoming Record,
				// so a local-only edit to a field the remote vCard doesn't
				// carry is silently discarded here. Confirmed intentional —
				// no model tracks per-field modified-since-sync
				// state, so a real merge isn't a small fix; pinned down by
				// TestReconcileContactSyncOverwritesLocalEditsOnRemoteChange.
				// Issue #395 makes the discard user-visible instead of silent:
				// the pre-sync (local) and post-apply (remote) field
				// snapshots are diffed against the link's last-synced
				// baseline and every overwritten local edit is recorded as a
				// ContactSyncConflict the UI can surface and restore.
				localSnapshot := syncConflictFieldSnapshot(&contact)
				models.ApplyRecordToContact(&contact, record, photoDir)
				// Issue #512: same fix-up as the create branch above -- clean
				// before the remote snapshot is taken, so both the conflict
				// record and the saved contact reflect sanitized data.
				SanitizeImportedContact(&contact)
				remoteSnapshot := syncConflictFieldSnapshot(&contact)
				if err := recordSyncConflicts(tx, sub, &contact, link, localSnapshot, remoteSnapshot, preSyncBaselines[link.ContactID]); err != nil {
					return err
				}
				if err := tx.Save(&contact).Error; err != nil {
					return err
				}
				link.ETag = obj.ETag
				link.ContentHash = hash
				link.SyncedValues = syncConflictSnapshotJSON(remoteSnapshot)
				if err := tx.Save(&link).Error; err != nil {
					return err
				}
				stats.Updated++
			}
		}

		allDeleted := append([]string(nil), deletedPaths...)
		if fullRefetch {
			// A full refetch has no explicit Deleted list (QueryAddressBook
			// just returns the address book's current membership) — any
			// existing link whose Href wasn't in that membership is a
			// contact the server no longer has.
			var existingLinks []models.ContactSyncLink
			if err := tx.Where("subscription_id = ?", sub.ID).Find(&existingLinks).Error; err != nil {
				return err
			}
			for _, l := range existingLinks {
				if _, ok := seenPaths[l.Href]; !ok {
					allDeleted = append(allDeleted, l.Href)
				}
			}
		}

		for _, path := range allDeleted {
			var link models.ContactSyncLink
			if err := tx.Where("subscription_id = ? AND href = ?", sub.ID, path).First(&link).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return err
			}
			var contact models.Contact
			cErr := tx.Where("id = ? AND user_id = ?", link.ContactID, sub.UserID).First(&contact).Error
			if errors.Is(cErr, gorm.ErrRecordNotFound) {
				// Contact already gone (e.g. hard-removed by an admin
				// tool); just drop the stale link.
				if err := tx.Delete(&link).Error; err != nil {
					return err
				}
				continue
			}
			if cErr != nil {
				return cErr
			}

			// Archive, not hard-delete — matches how Contact.Archived is
			// already used elsewhere (docs/adrs/0001-neutral-hub-and-spoke-contact-model.md explicit decision). Loading the row first
			// (rather than a bulk Model(&models.Contact{}).Where(...).Update)
			// matters: Contact.AfterSave (contact.go) recomputes ETag via
			// tx.Model(c) using the receiver's own ID/UpdatedAt, and a bulk
			// update's zero-value receiver has no ID, which GORM rejects as
			// a where-less update. contact_controller.go's ArchiveContact
			// uses this same loaded-row-then-Model(&contact).Update shape
			// for exactly that reason.
			if err := tx.Model(&contact).Update("archived", true).Error; err != nil {
				return err
			}
			if err := tx.Delete(&link).Error; err != nil {
				return err
			}
			stats.Archived++
		}

		return nil
	})
	if err != nil {
		return ContactSyncStats{}, fmt.Errorf("failed to import contacts: %w", err)
	}
	return stats, nil
}

func contactContentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
