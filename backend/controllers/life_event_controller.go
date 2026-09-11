package controllers

import (
	"errors"
	"fmt"
	"mycorrhizal/contactmodel"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/logger"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// defaultReminderHour is the hour (0-23) at which life-event reminders fire
// in the user's configured timezone. Centralized so a future per-user
// preference can reference it; not yet surfaced in the UI.
const defaultReminderHour = 9

// verifyOwnedContact confirms vcardUID references a Contact owned by userID
// — the same ownership check every entity that references Contact.VCardUID
// needs (mirrors Activity's own ContactIDs ownership check).
func verifyOwnedContact(c *gin.Context, db *gorm.DB, userID uint, vcardUID string) bool {
	var contact models.Contact
	if err := db.Where("vcard_uid = ? AND user_id = ?", vcardUID, userID).First(&contact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact").WithDetails("vcard_uid", vcardUID))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
		}
		return false
	}
	return true
}

// getContactIDByVCardUID resolves a VCardUID to a numeric Contact ID owned by
// the given user. Returns nil, nil if the contact does not exist; returns
// nil, error for any other DB failure (transient, connection, etc.) so the
// caller can decide whether to abort or fall through.
func getContactIDByVCardUID(db *gorm.DB, userID uint, vcardUID string) (*uint, error) {
	var contact models.Contact
	if err := db.Where("vcard_uid = ? AND user_id = ?", vcardUID, userID).First(&contact).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &contact.ID, nil
}

// eventHasMonthDay returns true when the PartialDate has both month and day
// populated, which is required for yearly recurrence (T5b).
func eventHasMonthDay(d *contactmodel.PartialDate) bool {
	if d == nil || d.Month == nil || d.Day == nil {
		return false
	}
	m := *d.Month
	day := *d.Day
	if m < 1 || m > 12 || day < 1 || day > 31 {
		return false
	}
	// Validate real calendar: April 31 and Feb 30 normalize silently in
	// time.Date; reject them here so the user gets feedback instead of a
	// silently-wrong reminder date.
	check := time.Date(2000, time.Month(m), day, 0, 0, 0, 0, time.UTC)
	return check.Month() == time.Month(m)
}

// nextRemindAt computes the next yearly reminder timestamp from month/day,
// using the given location for timezone-aware date boundary calculation.
func nextRemindAt(month, day int, now time.Time, loc *time.Location) time.Time {
	d := time.Date(now.Year(), time.Month(month), day, defaultReminderHour, 0, 0, 0, loc)
	if !d.After(now.In(loc)) {
		d = d.AddDate(1, 0, 0)
	}
	return d
}

// syncLifeEventReminder keeps a materialised Reminder row in sync with the
// LifeEvent's Remind flag. Hard-deletes any existing reminder for this event
// (the row is machine-synthesized, not user-authored — no soft-delete), then
// creates a new yearly one if Remind is true and the event has a valid
// month/day date.
func syncLifeEventReminder(tx *gorm.DB, userID uint, event *models.LifeEvent, now time.Time, loc *time.Location) error {
	// Hard-delete: machine-synthesized rows are join-like, not user-authored.
	// Prevents soft-delete accumulation on every toggle/edit of Remind.
	// N9: their delivery state goes too (the reminders are Unscoped()-removed
	// here, so the FK cascade would cover it — this explicit pass keeps the
	// manual-cascade checklist complete).
	if err := tx.Where("reminder_id IN (SELECT id FROM reminders WHERE life_event_id = ?)", event.ID).Delete(&models.NotificationDelivery{}).Error; err != nil {
		return err
	}
	if err := tx.Unscoped().Where("life_event_id = ?", event.ID).Delete(&models.Reminder{}).Error; err != nil {
		return err
	}

	if !event.Remind {
		return nil
	}
	if !eventHasMonthDay(event.Date) {
		return nil
	}

	contactID, err := getContactIDByVCardUID(tx, userID, event.EntityID)
	if err != nil {
		return err
	}
	if contactID == nil {
		return nil
	}

	month := *event.Date.Month
	day := *event.Date.Day
	remindAt := nextRemindAt(month, day, now, loc)

	contactName := event.EntityID // fallback: show VCardUID in message if name lookup fails
	var contact models.Contact
	if tx.Where("vcard_uid = ? AND user_id = ?", event.EntityID, userID).First(&contact).Error == nil {
		contactName = contact.Firstname + " " + contact.Lastname
	}

	reminder := models.Reminder{
		UserID:      userID,
		Message:     fmt.Sprintf("Life event — %s: %s", event.Type, contactName),
		RemindAt:    remindAt,
		Recurrence:  "yearly",
		ContactID:   contactID,
		LifeEventID: &event.ID,
	}
	if err := tx.Create(&reminder).Error; err != nil {
		return err
	}
	return nil
}

// CreateLifeEvent creates a new LifeEvent (life_event.go) for the
// authenticated user. Runs inside a transaction so the event and its
// materialised reminder stay in sync.
func CreateLifeEvent(c *gin.Context) {
	input, err := middleware.GetValidated[models.LifeEventInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	if !verifyOwnedContact(c, db, userID, input.EntityID) {
		return
	}

	cfg := currentConfig(c)
	loc := cfg.GetReminderLocation()
	now := time.Now().In(loc)

	var event models.LifeEvent
	txErr := db.Transaction(func(tx *gorm.DB) error {
		event = models.LifeEvent{
			UserID:           userID,
			EntityID:         input.EntityID,
			Type:             input.Type,
			Category:         input.Category,
			Date:             input.Date,
			Description:      input.Description,
			Source:           input.Source,
			RelatedEntityIDs: input.RelatedEntityIDs,
			Remind:           input.Remind,
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		return syncLifeEventReminder(tx, userID, &event, now, loc)
	})
	if txErr != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to save life event").WithError(txErr))
		return
	}

	// A married life event populates the card's wedding anniversary
	// (services/wedding_sync.go), so the card and the timeline stay in sync.
	// Sync failures are logged rather than aborting: the life event itself has
	// already been committed at this point, and the next contact save will
	// re-reconcile the anniversary from the card side.
	if event.Type == models.LifeEventTypeMarried {
		if err := services.SyncWeddingFromLifeEvent(db, userID, event.EntityID, event.Date); err != nil {
			logger.FromContext(c).Error().Err(err).Str("lifeEventID", event.ID).Msg("Error syncing wedding anniversary from life event")
		}
	}

	services.TriggerWebhooksAsync(c.Request.Context(), db, currentConfig(c), userID, "life_event.created", event)

	c.JSON(http.StatusCreated, gin.H{"message": "Life event created successfully", "life_event": event})
}

// GetLifeEvent returns one LifeEvent.
func GetLifeEvent(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var event models.LifeEvent
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Life event").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve life event").WithError(err))
		}
		return
	}

	c.JSON(http.StatusOK, event)
}

// ListLifeEvents returns the authenticated user's LifeEvents, cursor-
// paginated (T17), optionally filtered by ?entity_id=<Contact.VCardUID> in
// browse mode. The ?since= change feed ignores entity_id and returns every
// changed event — including soft-deleted ones (tombstones) — so a replica
// converges. `total` is deliberately gone: life events accumulate without
// bound and exact counts are the thing cursor pagination gives up.
func ListLifeEvents(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	params, err := GetCursorParams(c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}
	if err := CheckFeedCursorAge(c, params); err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	var events []models.LifeEvent

	if params.Since {
		query := db.Unscoped().Model(&models.LifeEvent{}).Where("user_id = ?", userID)
		if params.Cursor != nil {
			pred, t, idv := cursorPredicate("life_events", params.Cursor, params.Cursor.ID, false)
			query = query.Where(pred, t, idv)
		}
		query = cursorOrderBy(query, "life_events", false).Limit(params.Limit + 1)
		if err := query.Find(&events).Error; err != nil {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve life events").WithError(err))
			return
		}
		nextCursor := ""
		if len(events) > params.Limit {
			events = events[:params.Limit]
			nextCursor = EncodeCursor(events[len(events)-1].UpdatedAt, events[len(events)-1].ID)
		}
		for i := range events {
			events[i].Deleted = events[i].DeletedAt.Valid
		}
		c.JSON(http.StatusOK, gin.H{
			"life_events": events,
			"next_cursor": nextCursor,
			"limit":       params.Limit,
			"sync":        buildSyncMeta(SyncModeIncremental),
		})
		return
	}

	entityID := c.Query("entity_id")

	baseQuery := db.Model(&models.LifeEvent{}).Where("user_id = ?", userID)
	if entityID != "" {
		baseQuery = baseQuery.Where("entity_id = ?", entityID)
	}

	desc := params.Order == "desc"
	if params.Cursor != nil {
		pred, t, idv := cursorPredicate("life_events", params.Cursor, params.Cursor.ID, desc)
		baseQuery = baseQuery.Where(pred, t, idv)
	}

	if err := cursorOrderBy(baseQuery, "life_events", desc).
		Limit(params.Limit + 1).
		Find(&events).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve life events").WithError(err))
		return
	}
	nextCursor := ""
	if len(events) > params.Limit {
		events = events[:params.Limit]
		nextCursor = EncodeCursor(events[len(events)-1].UpdatedAt, events[len(events)-1].ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"life_events": events,
		"next_cursor": nextCursor,
		"limit":       params.Limit,
		"sync":        buildSyncMeta(SyncModeIncremental),
	})
}

// UpdateLifeEvent updates a LifeEvent inside a transaction so the event write
// and its materialised reminder stay in sync.
func UpdateLifeEvent(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var event models.LifeEvent
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Life event").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve life event").WithError(err))
		}
		return
	}

	// CON-01 (issue #456, ADR 0008): reject a stale conditional write before
	// touching the row. No-op when the client sent no If-Match header.
	if !checkIfMatch(c, event.Revision) {
		return
	}
	wasMarried := event.Type == models.LifeEventTypeMarried

	input, err := middleware.GetValidated[models.LifeEventInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	if !verifyOwnedContact(c, db, userID, input.EntityID) {
		return
	}

	cfg := currentConfig(c)
	loc := cfg.GetReminderLocation()
	now := time.Now().In(loc)

	event.EntityID = input.EntityID
	event.Type = input.Type
	event.Category = input.Category
	event.Date = input.Date
	event.Description = input.Description
	event.Source = input.Source
	event.RelatedEntityIDs = input.RelatedEntityIDs
	event.Remind = input.Remind

	txErr := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&event).Error; err != nil {
			return err
		}
		return syncLifeEventReminder(tx, userID, &event, now, loc)
	})
	if txErr != nil {
		if handleRevisionConflict(c, "Life event", txErr) {
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to save life event").WithError(txErr))
		return
	}

	// Keep the card's wedding anniversary in step with the married event: a
	// still-married event syncs its date; an event that stopped being married
	// (or had its date cleared) clears the anniversary. Sync failures are
	// logged and the response still succeeds — the next contact save will
	// re-reconcile any drift.
	if event.Type == models.LifeEventTypeMarried {
		if err := services.SyncWeddingFromLifeEvent(db, userID, event.EntityID, event.Date); err != nil {
			logger.FromContext(c).Error().Err(err).Str("lifeEventID", event.ID).Msg("Error syncing wedding anniversary from life event")
		}
	} else if wasMarried {
		if err := services.SyncWeddingFromLifeEvent(db, userID, event.EntityID, nil); err != nil {
			logger.FromContext(c).Error().Err(err).Str("lifeEventID", event.ID).Msg("Error clearing wedding anniversary from life event")
		}
	}

	services.TriggerWebhooksAsync(c.Request.Context(), db, currentConfig(c), userID, "life_event.updated", event)

	c.JSON(http.StatusOK, event)
}

// DeleteLifeEvent deletes a LifeEvent inside a transaction. The materialised
// reminder is hard-deleted (machine-synthesized row); the event itself is
// soft-deleted (user-authored content).
func DeleteLifeEvent(c *gin.Context) {
	id := c.Param("id")
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var event models.LifeEvent
	if err := db.Where("id = ? AND user_id = ?", id, userID).First(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Life event").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve life event").WithError(err))
		}
		return
	}

	// CON-01 (issue #456, ADR 0008): a conditional DELETE with a stale
	// If-Match revision is rejected before the row is touched.
	if !checkIfMatch(c, event.Revision) {
		return
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		// N9: clear this event's reminder delivery state alongside its
		// machine-synthesized reminders.
		if err := tx.Where("reminder_id IN (SELECT id FROM reminders WHERE life_event_id = ?)", event.ID).Delete(&models.NotificationDelivery{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("life_event_id = ?", event.ID).Delete(&models.Reminder{}).Error; err != nil {
			return err
		}
		return tx.Delete(&event).Error
	})
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to delete life event").WithError(err))
		return
	}

	// Deleting a married event clears the card's wedding anniversary.
	// A sync failure is logged and the delete still succeeds — the contact
	// still owns the anniversary on its card, and a subsequent card save will
	// re-reconcile it (which, since the event is gone, will clear it).
	if event.Type == models.LifeEventTypeMarried {
		if err := services.SyncWeddingFromLifeEvent(db, userID, event.EntityID, nil); err != nil {
			logger.FromContext(c).Error().Err(err).Str("lifeEventID", event.ID).Msg("Error clearing wedding anniversary from life event")
		}
	}

	services.TriggerWebhooksAsync(c.Request.Context(), db, currentConfig(c), userID, "life_event.deleted", gin.H{"id": event.ID})

	c.JSON(http.StatusOK, gin.H{"message": "Life event deleted"})
}
