package controllers

import (
	apperrors "mycorrhizal/errors"
	"mycorrhizal/models"
	"mycorrhizal/services"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetDashboard returns the M3 "today/overview" composite
// (M3): upcoming
// birthdays, a handful of random contacts, the user's favorites (issue
// #173), upcoming reminders (with the contact's display name embedded,
// N+1-free), and overdue cadences — the five blocks the web DashboardPage
// used to fire separately (plus a per-reminder contact fetch), and the
// single call the Android client's
// three background pollers (ReminderNotificationWorker, CadenceCheckWorker,
// BirthdayCheckWorker) can share.
//
// Read-only, no writes, no cache — a pure aggregation of the same
// per-user-scoped queries the individual endpoints already run, following
// the N2 briefing composite's pattern (briefing_controller.go).
func GetDashboard(c *gin.Context) {
	db := c.MustGet("db").(*gorm.DB)
	userID, ok := currentUserID(c)
	if !ok {
		return
	}

	now := reminderNow(c)

	birthdays, err := services.GetUpcomingBirthdays(db, userID, now)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve upcoming birthdays").WithError(err))
		return
	}

	randomContacts, err := fetchRandomContacts(db, userID)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve contacts").WithError(err))
		return
	}

	favoriteContacts, err := fetchFavoriteContacts(db, userID)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve favorite contacts").WithError(err))
		return
	}

	reminders, err := services.GetUpcomingReminders(db, userID, now)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to retrieve reminders").WithError(err))
		return
	}
	dashboardReminders := attachReminderContactNames(db, userID, reminders)

	overdue, err := services.ListOverdueCadences(db, userID, cadenceNow(c))
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to compute overdue cadences").WithError(err))
		return
	}

	reachOutSuggestions, err := services.ListReachOutSuggestions(db, userID)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to load reach-out suggestions").WithError(err))
		return
	}

	syncConflicts, err := services.ListContactSyncConflicts(db, userID)
	if err != nil {
		apperrors.AbortWithError(c, apperrors.ErrDatabase("Failed to load sync conflicts").WithError(err))
		return
	}

	resp := models.DashboardResponse{
		Birthdays:            birthdays,
		RandomContacts:       randomContacts,
		UpcomingReminders:    dashboardReminders,
		Overdue:              toDashboardOverdueCadences(overdue),
		Favorites:            favoriteContacts,
		ReachOutSuggestions:  reachOutSuggestions,
		ContactSyncConflicts: syncConflicts,
	}
	normalizeDashboardSlices(&resp)

	c.JSON(http.StatusOK, resp)
}

// fetchRandomContacts mirrors GetContactsRandom's query exactly
// (contact_controller.go) — same field selection, same archived filter, same
// RANDOM() ordering — so the dashboard composite's block is identical to
// what GET /contacts/random returns today.
func fetchRandomContacts(db *gorm.DB, userID uint) ([]models.ContactResponse, error) {
	selectedFields := []string{"ID", "firstname", "lastname", "nickname", "circles", "photo_thumbnail"}

	var contacts []models.Contact
	query := db.Model(&models.Contact{}).Where("user_id = ?", userID).Where("archived = ?", false).
		Select(selectedFields).
		Order("RANDOM()").
		Limit(5)

	if err := query.Find(&contacts).Error; err != nil {
		return nil, err
	}

	responses := make([]models.ContactResponse, len(contacts))
	for i, contact := range contacts {
		responses[i] = models.ContactResponse{
			Contact:        contact,
			PhotoThumbnail: contact.PhotoThumbnail,
		}
	}
	return responses, nil
}

// fetchFavoriteContacts returns up to limit non-archived favorites, ordered
// by name — the dashboard's quick-access favorites block (issue #173). Same
// field selection shape as fetchRandomContacts so the two blocks render
// identically; is_favorite is included in the projection so the flag is
// actually true on the wire (not GORM's zero-value false).
func fetchFavoriteContacts(db *gorm.DB, userID uint) ([]models.ContactResponse, error) {
	const limit = 10
	selectedFields := []string{"ID", "firstname", "lastname", "nickname", "circles", "photo_thumbnail", "is_favorite"}

	var contacts []models.Contact
	query := db.Model(&models.Contact{}).Where("user_id = ?", userID).
		Where("archived = ?", false).
		Where("is_favorite = ?", true).
		Select(selectedFields).
		Order("sort_name ASC, id ASC").
		Limit(limit)

	if err := query.Find(&contacts).Error; err != nil {
		return nil, err
	}

	responses := make([]models.ContactResponse, len(contacts))
	for i, contact := range contacts {
		responses[i] = models.ContactResponse{
			Contact:        contact,
			PhotoThumbnail: contact.PhotoThumbnail,
		}
	}
	return responses, nil
}

// attachReminderContactNames batch-resolves each reminder's contact display
// name in one query (design decision 2: nickname-preferred, falling back to
// firstname+lastname — the exact rule DashboardPage.tsx's getContactName
// used client-side). Deliberately separate from
// services.loadReminderContactNames (notification_service.go), which has no
// nickname preference — that helper serves push/email body text, a
// different, already-shipped convention this endpoint must not perturb.
func attachReminderContactNames(db *gorm.DB, userID uint, reminders []models.Reminder) []models.DashboardReminder {
	contactIDs := make([]uint, 0, len(reminders))
	for _, r := range reminders {
		if r.ContactID != nil {
			contactIDs = append(contactIDs, *r.ContactID)
		}
	}

	nameByID := make(map[uint]string, len(contactIDs))
	if len(contactIDs) > 0 {
		var contacts []models.Contact
		if err := db.Where("user_id = ? AND id IN ?", userID, contactIDs).Find(&contacts).Error; err == nil {
			for _, c := range contacts {
				name := strings.TrimSpace(c.Firstname + " " + c.Lastname)
				if c.Nickname != "" {
					name = strings.TrimSpace(c.Nickname + " " + c.Lastname)
				}
				nameByID[c.ID] = name
			}
		}
	}

	result := make([]models.DashboardReminder, len(reminders))
	for i, r := range reminders {
		result[i] = models.DashboardReminder{Reminder: r}
		if r.ContactID != nil {
			result[i].ContactName = nameByID[*r.ContactID]
		}
	}
	return result
}

// toDashboardOverdueCadences converts services.OverdueCadence to the
// models-local mirror DashboardResponse's wire shape uses (see
// models.DashboardOverdueCadence's doc comment for why).
func toDashboardOverdueCadences(overdue []services.OverdueCadence) []models.DashboardOverdueCadence {
	result := make([]models.DashboardOverdueCadence, len(overdue))
	for i, o := range overdue {
		result[i] = models.DashboardOverdueCadence{
			Policy: o.Policy,
			Health: models.BriefingCadenceHealth{
				HasQualifyingInteraction: o.Health.HasQualifyingInteraction,
				LastInteraction:          o.Health.LastInteraction,
				NextDue:                  o.Health.NextDue,
				OverdueBy:                o.Health.OverdueBy,
			},
			ContactID:      o.ContactID,
			ContactName:    o.ContactName,
			PhotoThumbnail: o.PhotoThumbnail,
		}
	}
	return result
}

// normalizeDashboardSlices replaces every nil block with an empty slice so
// the response always serializes as `[]`, never `null`/absent — the same
// normalizeBriefingSlices discipline briefing_controller.go documents
// (CLAUDE.md frontend trap 8).
func normalizeDashboardSlices(resp *models.DashboardResponse) {
	if resp.Birthdays == nil {
		resp.Birthdays = []models.Birthday{}
	}
	if resp.RandomContacts == nil {
		resp.RandomContacts = []models.ContactResponse{}
	}
	if resp.UpcomingReminders == nil {
		resp.UpcomingReminders = []models.DashboardReminder{}
	}
	if resp.Overdue == nil {
		resp.Overdue = []models.DashboardOverdueCadence{}
	}
	if resp.Favorites == nil {
		resp.Favorites = []models.ContactResponse{}
	}
	if resp.ReachOutSuggestions == nil {
		resp.ReachOutSuggestions = []models.ReachOutSuggestionResponse{}
	}
	if resp.ContactSyncConflicts == nil {
		resp.ContactSyncConflicts = []models.ContactSyncConflictResponse{}
	}
}
