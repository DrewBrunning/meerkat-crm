package controllers

import (
	"errors"
	apperrors "mycorrhizal/errors"
	"mycorrhizal/logger"
	"mycorrhizal/middleware"
	"mycorrhizal/models"
	"mycorrhizal/services"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func CreateNote(c *gin.Context) {
	// Get the database instance from the context
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Get contact ID from the request URL
	contactID, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}

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

	// Get validated input from validation middleware
	noteInput, err := middleware.GetValidated[models.NoteInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	// Create note from validated input
	note := models.Note{
		UserID:    userID,
		Content:   noteInput.Content,
		Date:      noteInput.Date,
		ContactID: &contact.ID,
	}
	if err := db.Create(&note).Error; err != nil {
		logger.FromContext(c).Error().Err(err).Msg("Error saving note to database")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to save note").WithError(err))
		return
	}

	services.TriggerWebhooksAsync(c.Request.Context(), db, currentConfig(c), userID, "note.created", note)
	c.JSON(http.StatusOK, gin.H{"message": "Note created successfully", "note": note})
}

func CreateUnassignedNote(c *gin.Context) {
	// Get the database instance from the context
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Get validated input from validation middleware
	noteInput, err := middleware.GetValidated[models.NoteInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	// Create note from validated input
	note := models.Note{
		UserID:    userID,
		Content:   noteInput.Content,
		Date:      noteInput.Date,
		ContactID: noteInput.ContactID,
	}

	if note.ContactID != nil {
		var contact models.Contact
		if err := db.Where("user_id = ?", userID).First(&contact, *note.ContactID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact"))
			} else {
				apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
			}
			return
		}
	}
	if err := db.Create(&note).Error; err != nil {
		logger.FromContext(c).Error().Err(err).Msg("Error saving unassigned note to database")
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to save note").WithError(err))
		return
	}

	services.TriggerWebhooksAsync(c.Request.Context(), db, currentConfig(c), userID, "note.created", note)
	c.JSON(http.StatusOK, gin.H{"message": "Note created successfully", "note": note})
}

func GetNote(c *gin.Context) {
	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	var note models.Note
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	if err := db.Where("user_id = ?", userID).First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Note").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve note").WithError(err))
		}
		return
	}

	c.JSON(http.StatusOK, note)
}

func GetUnassignedNotes(c *gin.Context) {
	var notes []models.Note
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

	// Change feed (?since=): every unassigned note changed after the cursor —
	// including soft-deleted ones (Unscoped, marked deleted:true) so a replica
	// can apply deletions. Search/date filters are deliberately ignored here:
	// a feed is a mirror, not a filtered view.
	if params.Since {
		query := db.Unscoped().Model(&models.Note{}).
			Where("notes.user_id = ? AND contact_id IS NULL", userID)
		if params.Cursor != nil {
			id, ok := parseCursorID(params.Cursor)
			if !ok {
				apperrors.AbortWithError(c, apperrors.ErrInvalidInput("since", "cursor id is malformed"))
				return
			}
			pred, t, idv := cursorPredicate("notes", params.Cursor, id, false)
			query = query.Where(pred, t, idv)
		}
		query = cursorOrderBy(query, "notes", false).Limit(params.Limit + 1)
		if err := query.Find(&notes).Error; err != nil {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve notes").WithError(err))
			return
		}
		nextCursor := ""
		if len(notes) > params.Limit {
			notes = notes[:params.Limit]
			nextCursor = EncodeCursor(notes[len(notes)-1].UpdatedAt, notes[len(notes)-1].ID)
		}
		for i := range notes {
			notes[i].Deleted = notes[i].DeletedAt.Valid
		}
		c.JSON(http.StatusOK, gin.H{
			"notes":       notes,
			"next_cursor": nextCursor,
			"limit":       params.Limit,
			"sync":        buildSyncMeta(SyncModeIncremental),
		})
		return
	}

	search := services.NormalizeSearchTerm(strings.ToLower(strings.TrimSpace(c.Query("search"))))
	fromDateStr := c.Query("fromDate")
	toDateStr := c.Query("toDate")

	baseQuery := db.Model(&models.Note{}).
		Where("notes.user_id = ? AND contact_id IS NULL", userID)

	// Apply date filters
	if fromDateStr != "" {
		if fromDate, err := time.Parse("2006-01-02", fromDateStr); err == nil {
			baseQuery = baseQuery.Where("notes.date >= ?", fromDate)
		}
	}
	if toDateStr != "" {
		if toDate, err := time.Parse("2006-01-02", toDateStr); err == nil {
			// Add one day to include the entire end date
			toDate = toDate.AddDate(0, 0, 1)
			baseQuery = baseQuery.Where("notes.date < ?", toDate)
		}
	}

	if search != "" {
		like := "%" + search + "%"
		baseQuery = baseQuery.Where("LOWER(content) LIKE ?", like)
	}

	// N4: the inbox needs a real queue depth, so this endpoint DOES return a
	// total — unlike the contact-scoped note list above, where T17 removed the
	// count because a contact's note history accumulates without bound.
	//
	// The two cases differ in kind. This query is already constrained to
	// `contact_id IS NULL`: the unfiled set is a queue the user drains, not a
	// history that grows forever, so counting it is bounded work on an indexed
	// predicate. And the count is the point of an inbox — the frontend chip
	// previously rendered `notes.length`, i.e. the number of rows on the
	// loaded page, so a user with more than one page of unfiled notes saw an
	// under-count that then grew as they clicked "Load more".
	//
	// Counted on the filtered query (search/date) but BEFORE the cursor
	// predicate is applied, so it reflects the whole result set the user is
	// paging through rather than the page they are on. Session() clones the
	// builder so the count does not pollute the Find below.
	var total int64
	if err := baseQuery.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to count unassigned notes").WithError(err))
		return
	}

	// T17: cursor pagination on (updated_at, id).
	desc := params.Order == "desc"
	if params.Cursor != nil {
		id, ok := parseCursorID(params.Cursor)
		if !ok {
			apperrors.AbortWithError(c, apperrors.ErrInvalidInput("cursor", "cursor id is malformed"))
			return
		}
		pred, t, idv := cursorPredicate("notes", params.Cursor, id, desc)
		baseQuery = baseQuery.Where(pred, t, idv)
	}

	if err := cursorOrderBy(baseQuery, "notes", desc).Limit(params.Limit + 1).Find(&notes).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve unassigned notes").WithError(err))
		return
	}
	nextCursor := ""
	if len(notes) > params.Limit {
		notes = notes[:params.Limit]
		nextCursor = EncodeCursor(notes[len(notes)-1].UpdatedAt, notes[len(notes)-1].ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"notes":       notes,
		"next_cursor": nextCursor,
		"limit":       params.Limit,
		"total":       total,
		"sync":        buildSyncMeta(SyncModeIncremental),
	})
}

func UpdateNote(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	var note models.Note

	// Retrieve the existing note from the database
	if err := db.Where("user_id = ?", userID).First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Note").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve note").WithError(err))
		}
		return
	}

	// CON-01 (issue #456, ADR 0008): reject a stale conditional write before
	// touching the row. No-op when the client sent no If-Match header.
	if !checkIfMatch(c, note.Revision) {
		return
	}

	// Get validated input from validation middleware
	updatedNote, err := middleware.GetValidated[models.NoteInput](c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	// Updateable fields
	note.Content = updatedNote.Content
	note.Date = updatedNote.Date
	note.ContactID = updatedNote.ContactID

	if note.ContactID != nil {
		var contact models.Contact
		if err := db.Where("user_id = ?", userID).First(&contact, *note.ContactID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact"))
			} else {
				apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
			}
			return
		}
	}

	if err := db.Save(&note).Error; err != nil {
		if handleRevisionConflict(c, "Note", err) {
			return
		}
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to update note").WithError(err))
		return
	}

	services.TriggerWebhooksAsync(c.Request.Context(), db, currentConfig(c), userID, "note.updated", note)
	c.JSON(http.StatusOK, gin.H{"message": "Note updated successfully", "note": note})
}

func DeleteNote(c *gin.Context) {
	id, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Check if note exists first
	var note models.Note
	if err := db.Where("user_id = ?", userID).First(&note, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Note").WithDetails("id", id))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve note").WithError(err))
		}
		return
	}

	// CON-01 (issue #456, ADR 0008): a conditional DELETE with a stale
	// If-Match revision is rejected before the row is touched.
	if !checkIfMatch(c, note.Revision) {
		return
	}

	if err := db.Delete(&note).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to delete note").WithError(err))
		return
	}

	services.TriggerWebhooksAsync(c.Request.Context(), db, currentConfig(c), userID, "note.deleted", gin.H{"id": note.ID})
	c.JSON(http.StatusOK, gin.H{"message": "Note deleted"})
}

// GetNotesForContact retrieves a contact's notes. M19: the plain
// "everything at once" preload grew cursor pagination plus search and
// from/to-date filters, mirroring the global GetUnassignedNotes handler — the
// per-contact list pages by (updated_at, id) exactly like its sibling. Unlike
// the change-feed variants there is deliberately no ?since= handling here: a
// contact-scoped feed has no consumer, and keeping the endpoint to
// filterable-paged browsing keeps it small. The 404-on-missing-contact
// contract is preserved (checked before any note query).
func GetNotesForContact(c *gin.Context) {
	// Get contact ID from the request URL
	contactID, ok := requirePathUintID(c, "id")
	if !ok {
		return
	}

	// Get the database instance from the context
	db := c.MustGet("db").(*gorm.DB)

	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	// Fetch the contact first to preserve the 404 contract and to scope the
	// note query to this user's contact (never trust the URL id alone).
	var contact models.Contact
	if err := db.Where("user_id = ?", userID).First(&contact, contactID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			apperrors.AbortWithError(c, apperrors.ErrNotFound("Contact").WithDetails("id", contactID))
		} else {
			apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contact").WithError(err))
		}
		return
	}

	params, err := GetCursorParams(c)
	if err != nil {
		apperrors.AbortWithError(c, err)
		return
	}

	search := services.NormalizeSearchTerm(strings.ToLower(strings.TrimSpace(c.Query("search"))))
	fromDateStr := c.Query("fromDate")
	toDateStr := c.Query("toDate")

	baseQuery := db.Model(&models.Note{}).
		Where("notes.user_id = ? AND notes.contact_id = ?", userID, contact.ID)

	// Apply date filters
	if fromDateStr != "" {
		if fromDate, err := time.Parse("2006-01-02", fromDateStr); err == nil {
			baseQuery = baseQuery.Where("notes.date >= ?", fromDate)
		}
	}
	if toDateStr != "" {
		if toDate, err := time.Parse("2006-01-02", toDateStr); err == nil {
			// Add one day to include the entire end date
			toDate = toDate.AddDate(0, 0, 1)
			baseQuery = baseQuery.Where("notes.date < ?", toDate)
		}
	}

	if search != "" {
		like := "%" + search + "%"
		baseQuery = baseQuery.Where("LOWER(notes.content) LIKE ?", like)
	}

	// T17: cursor pagination on (updated_at, id).
	var notes []models.Note
	desc := params.Order == "desc"
	if params.Cursor != nil {
		id, ok := parseCursorID(params.Cursor)
		if !ok {
			apperrors.AbortWithError(c, apperrors.ErrInvalidInput("cursor", "cursor id is malformed"))
			return
		}
		pred, t, idv := cursorPredicate("notes", params.Cursor, id, desc)
		baseQuery = baseQuery.Where(pred, t, idv)
	}

	if err := cursorOrderBy(baseQuery, "notes", desc).Limit(params.Limit + 1).Find(&notes).Error; err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve notes").WithError(err))
		return
	}
	nextCursor := ""
	if len(notes) > params.Limit {
		notes = notes[:params.Limit]
		nextCursor = EncodeCursor(notes[len(notes)-1].UpdatedAt, notes[len(notes)-1].ID)
	}

	c.JSON(http.StatusOK, gin.H{
		"notes":       notes,
		"next_cursor": nextCursor,
		"limit":       params.Limit,
	})
}
