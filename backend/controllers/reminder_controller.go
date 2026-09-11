package controllers

import (
	"errors"
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

func CreateReminder(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	contactID := c.Param("id")

	// Find the contact by the ID
	var contact models.Contact
	if err := db.Where("user_id = ?", userID).First(&contact, contactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact").WithDetails("id", contactID))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
		}
		return
	}

	// Get the validated reminder from context (already bound by ValidateJSONMiddleware)
	reminder, err := middleware.GetValidated[models.Reminder](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	// Assign the ContactID to the reminder to link it to the contact
	reminder.ContactID = &contact.ID
	reminder.UserID = userID

	// Set hours, minutes, seconds to 0 to ensure reminders are found when comparing for "until date".
	// RemindAt is day-granular in intent: a reminder "for 14 March" has no
	// meaningful time-of-day, and the daily job compares remind_at against the
	// day boundary (docs/adrs/0015-temporal-semantics.md). The zone carried
	// here is whatever the client's RFC3339 value had (commonly Z); the day
	// boundary is re-derived in the reminder zone at send time.
	reminder.RemindAt = time.Date(reminder.RemindAt.Year(),
		reminder.RemindAt.Month(),
		reminder.RemindAt.Day(), 0, 0, 0, 0, reminder.RemindAt.Location())

	// Save the new reminder to the database
	if err := db.Create(&reminder).Error; err != nil {
		logger.FromContext(c).Error().Err(err).Msg("Error saving reminder to database")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to save reminder").WithError(err))
		return
	}

	// Clear the Contact association to avoid including it in the response
	reminder.Contact = models.Contact{}

	c.JSON(http.StatusOK, gin.H{"message": "Reminder created successfully", "reminder": reminder})
}

func GetReminder(c *gin.Context) {
	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	var reminder models.Reminder
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	if err := db.Where("user_id = ?", userID).First(&reminder, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Reminder").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminder").WithError(err))
		}
		return
	}

	c.JSON(http.StatusOK, reminder)
}

func UpdateReminder(c *gin.Context) {
	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	var reminder models.Reminder
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	if err := db.Where("user_id = ?", userID).First(&reminder, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Reminder").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminder").WithError(err))
		}
		return
	}

	// CON-01 (issue #456, ADR 0008): reject a stale conditional write before
	// touching the row. No-op when the client sent no If-Match header.
	if !checkIfMatch(c, reminder.Revision) {
		return
	}

	// Get the validated reminder from context (already bound by ValidateJSONMiddleware)
	updatedReminder, err := middleware.GetValidated[models.Reminder](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	// Updateable fields
	reminder.Message = updatedReminder.Message
	reminder.ByMail = updatedReminder.ByMail
	reminder.RemindAt = time.Date(updatedReminder.RemindAt.Year(),
		updatedReminder.RemindAt.Month(),
		updatedReminder.RemindAt.Day(), 0, 0, 0, 0,
		updatedReminder.RemindAt.Location())
	reminder.Recurrence = updatedReminder.Recurrence
	reminder.ReoccurFromCompletion = updatedReminder.ReoccurFromCompletion
	reminder.ContactID = updatedReminder.ContactID

	if reminder.ContactID != nil {
		var contact models.Contact
		if err := db.Where("user_id = ?", userID).First(&contact, *reminder.ContactID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact"))
			} else {
				apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
			}
			return
		}
	}

	if err := db.Updates(&reminder).Error; err != nil {
		if handleRevisionConflict(c, "Reminder", err) {
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to update reminder").WithError(err))
		return
	}

	// Clear the Contact association to avoid including it in the response
	reminder.Contact = models.Contact{}

	c.JSON(http.StatusOK, gin.H{"message": "Reminder updated successfully", "reminder": reminder})
}

func DeleteReminder(c *gin.Context) {
	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Check if reminder exists first
	var reminder models.Reminder
	if err := db.Where("user_id = ?", userID).First(&reminder, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Reminder").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminder").WithError(err))
		}
		return
	}

	// CON-01 (issue #456, ADR 0008): a conditional DELETE with a stale
	// If-Match revision is rejected before the row is touched.
	if !checkIfMatch(c, reminder.Revision) {
		return
	}

	if err := services.DeleteNotificationDeliveries(db, []uint{reminder.ID}); err != nil {
		logger.FromContext(c).Error().Err(err).Uint("reminder_id", reminder.ID).Msg("Failed to clear notification deliveries for reminder")
	}

	if err := db.Delete(&reminder).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to delete reminder").WithError(err))
		return
	}

	// Issue #177: a reminder deleted directly (rather than completed/skipped
	// via CompleteReminder) still needs its linked ReachOutSuggestion
	// dismissed — otherwise the suggestion stays pending forever with a
	// reminder_id pointing at a now-nonexistent row.
	if err := services.DismissReachOutSuggestionByReminderID(db, userID, reminder.ID); err != nil {
		logger.FromContext(c).Error().Err(err).Uint("reminder_id", reminder.ID).Msg("Failed to dismiss reach-out suggestion for reminder")
	}

	c.JSON(http.StatusOK, gin.H{"message": "Reminder deleted"})
}

func GetRemindersForContact(c *gin.Context) {
	contactID, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}

	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var contact models.Contact

	if err := db.Preload("Reminders", "reminders.user_id = ?", userID).Where("user_id = ?", userID).First(&contact, contactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact").WithDetails("id", contactID))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"reminders": contact.Reminders,
	})
}

// GetAllReminders returns all reminders across all contacts for the current user
func GetAllReminders(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var reminders []models.Reminder

	// Get all reminders, ordered by remind_at date
	// Don't preload Contact to avoid validation issues with invalid contact data
	if err := db.Where("user_id = ?", userID).Order("remind_at ASC").Find(&reminders).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminders").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"reminders": reminders,
	})
}

// GetUpcomingReminders returns all reminders due within the next 7 days when that set exceeds five, otherwise it ensures at least five upcoming reminders overall
func GetUpcomingReminders(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	reminders, err := services.GetUpcomingReminders(db, userID, reminderNow(c))
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminders").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"reminders": reminders,
	})
}

// CompleteReminder marks a reminder as completed
// Use ?skip=true to skip without recording in timeline (for recurring reminders, reschedules to next occurrence)
func CompleteReminder(c *gin.Context) {
	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)
	skip := c.Query("skip") == "true"

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var reminder models.Reminder
	if err := db.Where("user_id = ?", userID).First(&reminder, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Reminder").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminder").WithError(err))
		}
		return
	}

	// Mark as completed
	reminder.Completed = true
	reminder.LastSent = new(time.Time)
	*reminder.LastSent = time.Now()

	// Create a completion record for the timeline (unless skipping)
	if !skip {
		completion := models.ReminderCompletion{
			UserID:      userID,
			ReminderID:  &reminder.ID,
			ContactID:   *reminder.ContactID,
			Message:     reminder.Message,
			CompletedAt: time.Now(),
		}
		if err := db.Create(&completion).Error; err != nil {
			logger.FromContext(c).Error().Err(err).Msg("Failed to create reminder completion record")
			// Don't fail the entire operation if completion record fails
		}
	}

	action := "completed"
	if skip {
		action = "skipped"
	}

	// If reoccur from completion, calculate next reminder time
	// Default to true if not specified (nil)
	reoccurFromCompletion := reminder.ReoccurFromCompletion == nil || *reminder.ReoccurFromCompletion
	rescheduled := false
	if reoccurFromCompletion && reminder.Recurrence != "once" {
		reminder.RemindAt = services.CalculateNextReminderTime(reminder)
		// Reset completed and email_sent flags for recurring reminders
		reminder.Completed = false
		reminder.EmailSent = false
		rescheduled = true

		logger.FromContext(c).Info().
			Time("next_remind_at", reminder.RemindAt).
			Uint("reminder_id", reminder.ID).
			Str("action", action).
			Msg("Reminder processed, next occurrence scheduled")
	}

	// Delete "once" reminders after completion
	if reminder.Recurrence == "once" {
		// N9: clear this occurrence's delivery state so no channel re-sends it
		if err := services.DeleteNotificationDeliveries(db, []uint{reminder.ID}); err != nil {
			logger.FromContext(c).Error().Err(err).Uint("reminder_id", reminder.ID).Msg("Failed to clear notification deliveries for completed reminder")
		}
		if err := db.Delete(&reminder).Error; err != nil {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to delete 'once' reminder").WithError(err))
			return
		}

		// Issue #177: dismiss the linked ReachOutSuggestion only now that the
		// reminder itself is confirmed deleted — firing this before the
		// delete/save could succeed would dismiss a suggestion for a
		// completion that then failed and left the reminder untouched.
		if err := services.DismissReachOutSuggestionByReminderID(db, userID, reminder.ID); err != nil {
			logger.FromContext(c).Error().Err(err).Uint("reminder_id", reminder.ID).Msg("Failed to dismiss reach-out suggestion for reminder")
		}

		logger.FromContext(c).Info().Uint("reminder_id", reminder.ID).Str("action", action).Msg("Deleted 'once' reminder")
		c.JSON(http.StatusOK, gin.H{"message": "Reminder " + action + " and deleted"})
		return
	}

	// Save the updated reminder
	if err := db.Save(&reminder).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to update reminder").WithError(err))
		return
	}

	// Issue #177: same ordering rule as the "once" branch above — only
	// dismiss once the save actually succeeded.
	if err := services.DismissReachOutSuggestionByReminderID(db, userID, reminder.ID); err != nil {
		logger.FromContext(c).Error().Err(err).Uint("reminder_id", reminder.ID).Msg("Failed to dismiss reach-out suggestion for reminder")
	}

	// N9: a rescheduled occurrence is a fresh reminder — clear the previous
	// occurrence's delivery records so every enabled channel notifies again
	// (mirrors the email_sent=false reset above).
	if rescheduled {
		if err := services.DeleteNotificationDeliveries(db, []uint{reminder.ID}); err != nil {
			logger.FromContext(c).Error().Err(err).Uint("reminder_id", reminder.ID).Msg("Failed to clear notification deliveries for rescheduled reminder")
		}
	}

	// Clear the Contact association to avoid including it in the response
	reminder.Contact = models.Contact{}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Reminder " + action + " successfully",
		"reminder": reminder,
	})
}

// GetCompletionsForContact returns all reminder completions for a specific contact
func GetCompletionsForContact(c *gin.Context) {
	contactID, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Verify contact belongs to user
	var contact models.Contact
	if err := db.Where("user_id = ?", userID).First(&contact, contactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact").WithDetails("id", contactID))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
		}
		return
	}

	var completions []models.ReminderCompletion
	if err := db.Where("user_id = ? AND contact_id = ?", userID, contactID).
		Order("completed_at DESC").
		Find(&completions).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminder completions").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"completions": completions,
	})
}

// DeleteCompletion deletes a reminder completion record
func DeleteCompletion(c *gin.Context) {
	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	var completion models.ReminderCompletion
	if err := db.Where("user_id = ?", userID).First(&completion, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Reminder completion").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminder completion").WithError(err))
		}
		return
	}

	if err := db.Delete(&completion).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to delete reminder completion").WithError(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Reminder completion deleted"})
}
