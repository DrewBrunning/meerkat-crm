package services

import (
	"encoding/json"
	"errors"
	"fmt"

	apperrors "mycorrhizal/errors"
	"mycorrhizal/logger"
	"mycorrhizal/models"

	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// CardDAV sync conflicts (issue #395) — the usability half of the sync-health
// work. reconcileContactSync applies remote vCard changes by documented
// full-replace policy: a local edit to a field the remote vCard doesn't
// carry (or carries a different value for) is silently discarded. These
// functions give that silence a voice:
//
//   - Each ContactSyncLink now stores a per-field snapshot of the values the
//     last sync wrote (SyncedValues). On the next sync, a field whose current
//     value differs from that snapshot is by definition a *local* edit; if the
//     incoming remote value differs from it too, the local edit is about to be
//     overwritten, and a ContactSyncConflict row is recorded (field, local
//     value, remote value) instead of losing it silently.
//   - ListContactSyncConflicts / DismissContactSyncConflict /
//     RestoreContactSyncConflict back the UI: a "recent sync conflicts" list
//     that can re-apply the local value (restore) or acknowledge the remote
//     one (dismiss). Restoring writes the flat field back through the normal
//     contact save path, so the T75 Card merge and the T18 audit trail both
//     run on it like any other edit.
//
// Two deliberate boundaries:
//
//   - A missing baseline is backfilled from the pre-fetch state, not treated
//     as "no conflicts". A link whose SyncedValues is empty predates
//     migration 000032 (or lost its baseline), so the reconcile has no stored
//     reference to diff against. It used to return early there, which meant a
//     REST edit that landed during the network-fetch window was full-replaced
//     with no conflict row — silently losing it (issue #919). Instead,
//     capturePreSyncBaselines snapshots each such contact *before* the fetch
//     and uses that as this run's effective baseline: a field that changed
//     between the snapshot and the pre-apply read is, by definition, a
//     concurrent local edit; a field already divergent before the sync started
//     is part of the seeded baseline and is not retroactively reported.
//   - Restore does not rewrite the link's SyncedValues. The restored value is
//     still divergent from the remote, so the next real remote change
//     re-conflicts — honest, rather than silently re-losing the edit.
// ---------------------------------------------------------------------------

// syncConflictFieldKeys is the canonical list of flat contact fields the
// sync conflict detector tracks, in a stable order. MUST stay in sync by
// hand with models.SyncConflictField* (the wire tokens) and the frontend's
// `syncConflicts.field.*` i18n labels — there is no dynamic type-list
// endpoint in this codebase, by design.
var syncConflictFieldKeys = []string{
	models.SyncConflictFieldFirstname,
	models.SyncConflictFieldLastname,
	models.SyncConflictFieldMiddlename,
	models.SyncConflictFieldPrefix,
	models.SyncConflictFieldSuffix,
	models.SyncConflictFieldNickname,
	models.SyncConflictFieldOrganization,
	models.SyncConflictFieldDepartment,
	models.SyncConflictFieldJobTitle,
	models.SyncConflictFieldRole,
	models.SyncConflictFieldEmail,
	models.SyncConflictFieldPhone,
	models.SyncConflictFieldAddress,
	models.SyncConflictFieldURL,
	models.SyncConflictFieldIMPP,
	models.SyncConflictFieldBirthday,
	models.SyncConflictFieldAnniversary,
	models.SyncConflictFieldCircles,
	models.SyncConflictFieldHowWeMet,
	models.SyncConflictFieldWorkInformation,
	models.SyncConflictFieldContactInformation,
}

// syncConflictFieldSnapshot renders the flat fields a user can edit (and the
// CardDAV full-replace can overwrite) as a stable map of field key -> value.
// Array fields (email/phone/address/url/impp/circles) are encoded as their
// JSON array with nil normalized to `[]`, so an empty array compares equal
// whether it was loaded from a `[]` or a `null` JSON column.
func syncConflictFieldSnapshot(c *models.Contact) map[string]string {
	snap := make(map[string]string, len(syncConflictFieldKeys))
	set := func(key, value string) { snap[key] = value }

	jsonVal := func(key string, v any) {
		encoded := syncConflictJSON(v)
		snap[key] = encoded
	}

	set(models.SyncConflictFieldFirstname, c.Firstname)
	set(models.SyncConflictFieldLastname, c.Lastname)
	set(models.SyncConflictFieldMiddlename, c.MiddleName)
	set(models.SyncConflictFieldPrefix, c.Prefix)
	set(models.SyncConflictFieldSuffix, c.Suffix)
	set(models.SyncConflictFieldNickname, c.Nickname)
	set(models.SyncConflictFieldOrganization, c.Organization)
	set(models.SyncConflictFieldDepartment, c.Department)
	set(models.SyncConflictFieldJobTitle, c.JobTitle)
	set(models.SyncConflictFieldRole, c.Role)
	set(models.SyncConflictFieldBirthday, c.Birthday)
	set(models.SyncConflictFieldAnniversary, c.Anniversary)
	set(models.SyncConflictFieldHowWeMet, c.HowWeMet)
	set(models.SyncConflictFieldWorkInformation, c.WorkInformation)
	set(models.SyncConflictFieldContactInformation, c.ContactInformation)

	for _, key := range []struct {
		key string
		v   any
	}{
		{models.SyncConflictFieldEmail, c.Emails},
		{models.SyncConflictFieldPhone, c.Phones},
		{models.SyncConflictFieldAddress, c.Addresses},
		{models.SyncConflictFieldURL, c.URLs},
		{models.SyncConflictFieldIMPP, c.IMPPs},
		{models.SyncConflictFieldCircles, c.Circles},
	} {
		jsonVal(key.key, key.v)
	}

	return snap
}

// syncConflictJSON marshals v, normalizing any empty/nil slice to `[]` so the
// snapshot is canonical regardless of how a JSON column was loaded. These are
// all concrete marshal-safe types, so the JSON encoding cannot fail.
func syncConflictJSON(v any) string {
	switch val := v.(type) {
	case []models.ContactEmail:
		if len(val) == 0 {
			return "[]"
		}
	case []models.ContactPhone:
		if len(val) == 0 {
			return "[]"
		}
	case []models.ContactAddress:
		if len(val) == 0 {
			return "[]"
		}
	case []models.ContactURL:
		if len(val) == 0 {
			return "[]"
		}
	case []models.ContactIMPP:
		if len(val) == 0 {
			return "[]"
		}
	case []string:
		if len(val) == 0 {
			return "[]"
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// syncConflictSnapshotJSON encodes a snapshot map for storage on
// ContactSyncLink.SyncedValues.
func syncConflictSnapshotJSON(snap map[string]string) string {
	b, err := json.Marshal(snap)
	if err != nil {
		// snapshot values are all strings from our own encoder; this cannot
		// fail in practice, but degrade to empty rather than panicking.
		return ""
	}
	return string(b)
}

// parseSyncConflictSnapshot decodes a stored SyncedValues baseline.
func parseSyncConflictSnapshot(raw string) (map[string]string, error) {
	snap := make(map[string]string, len(syncConflictFieldKeys))
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, err
	}
	return snap, nil
}

// diffSyncConflictFields compares the local (current), synced (last-written or
// pre-sync), and remote (incoming) snapshots and returns a conflict for every
// field where a local edit is about to be overwritten: local differs from
// synced (so it is a local edit) AND local differs from remote (so it would
// change). A nil synced map means there is no usable reference at all (the
// direct-call case with a missing/corrupt baseline and no pre-sync fallback);
// reporting nothing there preserves the "no retroactive conflict spam" stance
// for callers that never had a baseline to begin with.
func diffSyncConflictFields(local, synced, remote map[string]string) []models.ContactSyncConflict {
	if synced == nil {
		return nil
	}
	var conflicts []models.ContactSyncConflict
	for _, key := range syncConflictFieldKeys {
		localVal, remoteVal := local[key], remote[key]
		if localVal != synced[key] && localVal != remoteVal {
			conflicts = append(conflicts, models.ContactSyncConflict{
				Field:       key,
				LocalValue:  localVal,
				RemoteValue: remoteVal,
				Status:      models.SyncConflictStatusPending,
			})
		}
	}
	return conflicts
}

// capturePreSyncBaselines returns, for every link of sub whose stored
// SyncedValues baseline is missing or unparseable, the linked contact's
// flat-field snapshot taken *now* — before the network fetch that precedes
// reconcileContactSyncWithBaselines.
//
// This is the issue #919 fix. reconcileContactSync normally diffs the
// pre-apply local state against the link's stored baseline to tell a local
// edit from a plain remote change; a link without a usable baseline has
// nothing to diff against. Seeding the effective baseline with the state
// observed before the fetch closes the concurrent-edit window: any field that
// changes between this snapshot and the pre-apply read was edited while the
// sync was still fetching. It is keyed by contact ID and returns nil (no extra
// contact reads) when every link already has a parseable baseline — the common
// case.
func capturePreSyncBaselines(db *gorm.DB, sub *models.ContactSubscription) (map[uint]map[string]string, error) {
	var links []models.ContactSyncLink
	if err := db.Model(&models.ContactSyncLink{}).
		Select("contact_id", "synced_values").
		Where("subscription_id = ?", sub.ID).
		Find(&links).Error; err != nil {
		return nil, fmt.Errorf("loading sync links for baseline capture: %w", err)
	}

	var needsContact []uint
	for _, link := range links {
		if link.SyncedValues == "" {
			needsContact = append(needsContact, link.ContactID)
			continue
		}
		if _, err := parseSyncConflictSnapshot(link.SyncedValues); err != nil {
			needsContact = append(needsContact, link.ContactID)
		}
	}
	if len(needsContact) == 0 {
		return nil, nil
	}

	var contacts []models.Contact
	if err := db.Where("user_id = ? AND id IN ?", sub.UserID, needsContact).Find(&contacts).Error; err != nil {
		return nil, fmt.Errorf("loading contacts for baseline capture: %w", err)
	}
	baselines := make(map[uint]map[string]string, len(contacts))
	for i := range contacts {
		baselines[contacts[i].ID] = syncConflictFieldSnapshot(&contacts[i])
	}
	return baselines, nil
}

// recordSyncConflicts is called from reconcileContactSync (inside its
// transaction) on the update path: given the pre-sync (local) and post-apply
// (remote) snapshots and the link's baseline, it persists one
// ContactSyncConflict per overwritten local edit.
//
// preSyncBaseline is the contact's pre-fetch snapshot captured by
// capturePreSyncBaselines, and is only consulted when the link's own stored
// baseline is missing or unparseable. Using it there turns a concurrent
// local edit (one that landed during the network fetch) into a detectable
// delta rather than a silent loss (issue #919). A nil preSyncBaseline leaves
// the old "no baseline, no conflicts" behavior for direct callers.
func recordSyncConflicts(tx *gorm.DB, sub *models.ContactSubscription, contact *models.Contact, link models.ContactSyncLink, local, remote, preSyncBaseline map[string]string) error {
	var synced map[string]string
	if link.SyncedValues != "" {
		parsed, err := parseSyncConflictSnapshot(link.SyncedValues)
		if err != nil {
			// A corrupt baseline should not fail the whole sync; fall through
			// to the pre-sync snapshot (or no conflicts) and rebuild it on
			// the next run.
			logger.Warn().Err(err).Uint("subscription_id", sub.ID).Uint("contact_id", contact.ID).
				Msg("Sync conflict detection: unparseable synced-values baseline, using pre-sync self-baseline")
		} else {
			synced = parsed
		}
	}
	if synced == nil {
		synced = preSyncBaseline
	}
	for _, conflict := range diffSyncConflictFields(local, synced, remote) {
		conflict.UserID = sub.UserID
		conflict.SubscriptionID = sub.ID
		conflict.ContactID = contact.ID
		if err := tx.Create(&conflict).Error; err != nil {
			return fmt.Errorf("recording sync conflict: %w", err)
		}
	}
	return nil
}

// ListContactSyncConflicts returns every pending sync conflict for the user,
// newest first, enriched with the contact's display name/vcard UID/photo and
// the subscription's name in two follow-up queries (the same N+1-avoidance
// shape ListReachOutSuggestions uses). A conflict whose contact is archived
// or gone is silently dropped — there is nothing to act on for a contact you
// cannot open.
func ListContactSyncConflicts(db *gorm.DB, userID uint) ([]models.ContactSyncConflictResponse, error) {
	var conflicts []models.ContactSyncConflict
	if err := db.Where("user_id = ? AND status = ?", userID, models.SyncConflictStatusPending).
		Order("created_at DESC").Find(&conflicts).Error; err != nil {
		return nil, fmt.Errorf("loading sync conflicts: %w", err)
	}
	if len(conflicts) == 0 {
		return nil, nil
	}

	contactIDs := make([]uint, 0, len(conflicts))
	subscriptionIDs := make([]uint, 0, len(conflicts))
	for _, conflict := range conflicts {
		contactIDs = append(contactIDs, conflict.ContactID)
		subscriptionIDs = append(subscriptionIDs, conflict.SubscriptionID)
	}

	contactByID := make(map[uint]models.Contact, len(contactIDs))
	var contacts []models.Contact
	if err := db.Where("user_id = ? AND id IN ? AND archived = ?", userID, contactIDs, false).Find(&contacts).Error; err != nil {
		return nil, fmt.Errorf("loading contacts for sync conflicts: %w", err)
	}
	for _, c := range contacts {
		contactByID[c.ID] = c
	}

	subByID := make(map[uint]models.ContactSubscription, len(subscriptionIDs))
	var subs []models.ContactSubscription
	if err := db.Where("user_id = ? AND id IN ?", userID, subscriptionIDs).Find(&subs).Error; err != nil {
		return nil, fmt.Errorf("loading subscriptions for sync conflicts: %w", err)
	}
	for _, s := range subs {
		subByID[s.ID] = s
	}

	result := make([]models.ContactSyncConflictResponse, 0, len(conflicts))
	for _, conflict := range conflicts {
		c, ok := contactByID[conflict.ContactID]
		if !ok {
			continue
		}
		result = append(result, models.ContactSyncConflictResponse{
			ContactSyncConflict: conflict,
			ContactID:           c.ID,
			ContactVCardUID:     c.VCardUID,
			ContactName:         displayContactName(c),
			PhotoThumbnail:      c.PhotoThumbnail,
			SubscriptionName:    subByID[conflict.SubscriptionID].Name,
		})
	}
	return result, nil
}

// RestoreContactSyncConflict re-applies a conflict's overwritten local value
// onto the contact, then marks the conflict dismissed. The write goes through
// the normal contact save path (T75 Card merge + T18 audit fire like any
// other edit). Non-pending conflicts are rejected with 409: the local value
// is only offered back once, before the user has acted. See the file doc for
// why SyncedValues is intentionally NOT updated here.
func RestoreContactSyncConflict(db *gorm.DB, userID uint, id string) error {
	var conflict models.ContactSyncConflict
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&conflict).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.ErrNotFound("Sync conflict").WithDetails("id", id)
		}
		return fmt.Errorf("loading sync conflict: %w", err)
	}
	if conflict.Status == models.SyncConflictStatusDismissed {
		return apperrors.ErrConflict("sync conflict already resolved")
	}

	var contact models.Contact
	if err := db.Where("id = ? AND user_id = ?", conflict.ContactID, userID).First(&contact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.ErrNotFound("Contact").WithDetails("id", conflict.ContactID)
		}
		return fmt.Errorf("loading sync conflict contact: %w", err)
	}

	if err := restoreConflictFieldValue(&contact, conflict.Field, conflict.LocalValue); err != nil {
		return apperrors.ErrOperationFailed("Restore sync conflict", err.Error())
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&contact).Error; err != nil {
			return fmt.Errorf("restoring sync conflict value: %w", err)
		}
		if err := tx.Model(&conflict).Update("status", models.SyncConflictStatusDismissed).Error; err != nil {
			return fmt.Errorf("resolving sync conflict: %w", err)
		}
		return nil
	})
}

// restoreConflictFieldValue applies a stored conflict LocalValue back onto
// the matching flat field of a Contact — the inverse of the snapshot
// encoding. Array fields unmarshal their JSON; scalar fields assign directly.
func restoreConflictFieldValue(c *models.Contact, field, value string) error {
	switch field {
	case models.SyncConflictFieldFirstname:
		c.Firstname = value
	case models.SyncConflictFieldLastname:
		c.Lastname = value
	case models.SyncConflictFieldMiddlename:
		c.MiddleName = value
	case models.SyncConflictFieldPrefix:
		c.Prefix = value
	case models.SyncConflictFieldSuffix:
		c.Suffix = value
	case models.SyncConflictFieldNickname:
		c.Nickname = value
	case models.SyncConflictFieldOrganization:
		c.Organization = value
	case models.SyncConflictFieldDepartment:
		c.Department = value
	case models.SyncConflictFieldJobTitle:
		c.JobTitle = value
	case models.SyncConflictFieldRole:
		c.Role = value
	case models.SyncConflictFieldBirthday:
		c.Birthday = value
	case models.SyncConflictFieldAnniversary:
		c.Anniversary = value
	case models.SyncConflictFieldHowWeMet:
		c.HowWeMet = value
	case models.SyncConflictFieldWorkInformation:
		c.WorkInformation = value
	case models.SyncConflictFieldContactInformation:
		c.ContactInformation = value
	case models.SyncConflictFieldEmail:
		var v []models.ContactEmail
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("parsing email value: %w", err)
		}
		c.Emails = v
	case models.SyncConflictFieldPhone:
		var v []models.ContactPhone
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("parsing phone value: %w", err)
		}
		c.Phones = v
	case models.SyncConflictFieldAddress:
		var v []models.ContactAddress
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("parsing address value: %w", err)
		}
		c.Addresses = v
	case models.SyncConflictFieldURL:
		var v []models.ContactURL
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("parsing url value: %w", err)
		}
		c.URLs = v
	case models.SyncConflictFieldIMPP:
		var v []models.ContactIMPP
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("parsing impp value: %w", err)
		}
		c.IMPPs = v
	case models.SyncConflictFieldCircles:
		var v []string
		if err := json.Unmarshal([]byte(value), &v); err != nil {
			return fmt.Errorf("parsing circles value: %w", err)
		}
		c.Circles = v
	default:
		return fmt.Errorf("unknown sync conflict field %q", field)
	}
	return nil
}

// DismissContactSyncConflict marks a conflict dismissed without restoring.
// Idempotent: an already-dismissed conflict is a no-op success, not an error.
func DismissContactSyncConflict(db *gorm.DB, userID uint, id string) error {
	var conflict models.ContactSyncConflict
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&conflict).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.ErrNotFound("Sync conflict").WithDetails("id", id)
		}
		return fmt.Errorf("loading sync conflict: %w", err)
	}
	if conflict.Status == models.SyncConflictStatusDismissed {
		return nil
	}
	if err := db.Model(&conflict).Update("status", models.SyncConflictStatusDismissed).Error; err != nil {
		return fmt.Errorf("dismissing sync conflict: %w", err)
	}
	return nil
}
