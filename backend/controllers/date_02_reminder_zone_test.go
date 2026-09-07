package controllers

import (
	"encoding/json"
	"mycorrhizal/config"
	"mycorrhizal/models"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- DATE-02 (issue #483): the interactive endpoints and the reminder zone ---
//
// docs/adrs/0015-temporal-semantics.md Rule 4 records a known divergence these
// tests close: the scheduled digest computes "today" in REMINDER_TIMEZONE, but
// the interactive endpoints (GET /contacts/birthdays, the dashboard composite,
// GET /reminders/upcoming, the briefing) used the server's own local zone via a
// bare time.Now(). On a host whose local zone differs from REMINDER_TIMEZONE
// the UI could disagree with the email by a day. reminderNow(c) is now the one
// source of "today" for every handler, and every test below drives it with a
// fixed injected instant (the timeNow seam), so they pass on any calendar day.

// setTimeNow pins the controllers' clock to a fixed instant for the duration of
// the test. Controller tests are sequential (no t.Parallel), so the swap cannot
// leak across tests.
func setTimeNow(t *testing.T, instant time.Time) {
	t.Helper()
	orig := timeNow
	timeNow = func() time.Time { return instant }
	t.Cleanup(func() { timeNow = orig })
}

// swapLocal rotates the process's Local zone for the duration of the test, to
// prove a behavior does not depend on the server's own zone. It restores the
// original zone (as a value) on cleanup.
func swapLocal(t *testing.T, zone string) {
	t.Helper()
	orig := time.Local
	loc, err := time.LoadLocation(zone)
	require.NoError(t, err)
	time.Local = loc
	t.Cleanup(func() { time.Local = orig })
}

// zoneRouter builds an authenticated router whose config carries the given
// REMINDER_TIMEZONE, on top of the shared seeded test DB.
func zoneRouter(t *testing.T, db *gorm.DB, tz string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	var user models.User
	require.NoError(t, db.First(&user).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Set("userID", user.ID)
		c.Set("cfg", config.Config{ReminderTimezone: tz})
		c.Next()
	})
	return router
}

// TestBirthdaysAndDashboard_UsesReminderZoneForToday pins both the birthdays
// endpoint and the dashboard composite's birthdays block against a fixed
// instant that is March 14 in New York but already March 15 in Kiritimati
// (UTC+14). A stored --03-14 birthday must be "today" (fetched) under the New
// York clock and a year out (absent) under the Kiritimati clock — and the
// stored --03-14 string must round-trip unchanged in both, never zone-shifted.
func TestBirthdaysAndDashboard_UsesReminderZoneForToday(t *testing.T) {
	// 2026-03-14 23:30 UTC == Mar 14 19:30 America/New_York (EDT) ==
	// Mar 15 13:30 Pacific/Kiritimati (UTC+14).
	instant := time.Date(2026, 3, 14, 23, 30, 0, 0, time.UTC)

	zones := []struct {
		zone       string
		wantMember bool // is the --03-14 birthday "today" (inside the fetch window)?
	}{
		{zone: "America/New_York", wantMember: true},
		{zone: "Pacific/Kiritimati", wantMember: false},
	}

	for _, z := range zones {
		t.Run(z.zone, func(t *testing.T) {
			setTimeNow(t, instant)

			db, _ := setupRouter()
			contact := models.Contact{UserID: seedUserID(t, db), Firstname: "Boundary", Lastname: "Case", Birthday: "--03-14", Archived: false}
			require.NoError(t, db.Create(&contact).Error)

			router := zoneRouter(t, db, z.zone)
			router.GET("/contacts/birthdays", GetUpcomingBirthdays)
			router.GET("/dashboard", GetDashboard)

			t.Run("birthdays endpoint", func(t *testing.T) {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/contacts/birthdays", nil)
				router.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code)

				var body struct {
					Birthdays []models.Birthday `json:"birthdays"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

				if z.wantMember {
					require.Len(t, body.Birthdays, 1, "the Mar-14 birthday is today under %s", z.zone)
					assert.Equal(t, "--03-14", body.Birthdays[0].Birthday, "the stored value must round-trip unchanged, never zone-shifted")
					assert.Equal(t, "Boundary Case", body.Birthdays[0].Name)
				} else {
					assert.Empty(t, body.Birthdays, "the same instant is Mar 15 under %s, so the Mar-14 birthday has wrapped out of the window", z.zone)
				}
			})

			t.Run("dashboard composite birthdays block", func(t *testing.T) {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("GET", "/dashboard", nil)
				router.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code)

				var dash models.DashboardResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dash))

				if z.wantMember {
					require.Len(t, dash.Birthdays, 1, "the dashboard birthdays block must agree with the birthdays endpoint under %s", z.zone)
					assert.Equal(t, "--03-14", dash.Birthdays[0].Birthday)
				} else {
					assert.Empty(t, dash.Birthdays)
				}
			})
		})
	}
}

// TestBirthdaysEndpoint_IndependentOfServerLocalZone rotates the process's
// Local zone across four very different zones while the endpoint is driven at a
// fixed instant against a fixed reminder zone (Asia/Kolkata). The response must
// be byte-identical every time: a date-only value never shifts, and the server
// host's own zone is never consulted. Reverting an endpoint to a bare
// time.Now() would make it read the real current date instead of the pinned
// instant and fail here.
func TestBirthdaysEndpoint_IndependentOfServerLocalZone(t *testing.T) {
	instant := time.Date(2026, 6, 15, 4, 0, 0, 0, time.UTC) // Jun 15 09:30 in Asia/Kolkata
	setTimeNow(t, instant)

	stored := []string{"--06-15", "1990-06-15", "1985-07-04"}
	var wantBody string

	for _, localZone := range []string{"UTC", "Pacific/Auckland", "America/New_York", "Pacific/Kiritimati"} {
		t.Run(localZone, func(t *testing.T) {
			swapLocal(t, localZone)

			db, _ := setupRouter()
			uid := seedUserID(t, db)
			for _, b := range stored {
				require.NoError(t, db.Create(&models.Contact{UserID: uid, Firstname: "Stored", Lastname: b, Birthday: b, Archived: false}).Error)
			}
			// A stored birthday outside the June/July fetch window must stay
			// excluded under every server zone too.
			require.NoError(t, db.Create(&models.Contact{UserID: uid, Firstname: "Out", Lastname: "OfWindow", Birthday: "1990-12-31", Archived: false}).Error)

			router := zoneRouter(t, db, "Asia/Kolkata")
			router.GET("/contacts/birthdays", GetUpcomingBirthdays)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/contacts/birthdays", nil)
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)

			// The three in-window birthdays must surface with their stored
			// string byte-identical; the out-of-window one must not.
			var body struct {
				Birthdays []models.Birthday `json:"birthdays"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Len(t, body.Birthdays, len(stored), "the June/July-window birthdays must surface regardless of server zone")
			got := make(map[string]bool, len(body.Birthdays))
			for _, b := range body.Birthdays {
				got[b.Birthday] = true
			}
			for _, s := range stored {
				assert.True(t, got[s], "stored birthday %q must round-trip unchanged under server zone %s", s, localZone)
			}
			assert.False(t, got["1990-12-31"], "an out-of-window birthday must stay excluded under server zone %s", localZone)

			if wantBody == "" {
				wantBody = w.Body.String()
			} else {
				assert.Equal(t, wantBody, w.Body.String(), "the birthdays response must not depend on the server's local zone")
			}
		})
	}
}

// TestContactBriefing_UpcomingDatesUseReminderZone pins that the briefing's
// upcoming-dates block (DaysUntilBirthday against "today") is computed in the
// reminder zone, not the server-local zone: the same fixed instant yields
// days_until 0 in Kiritimati (Mar 15 is today) and 1 in New York (still
// Mar 14) for a --03-15 birthday.
func TestContactBriefing_UpcomingDatesUseReminderZone(t *testing.T) {
	instant := time.Date(2026, 3, 14, 23, 30, 0, 0, time.UTC)

	zones := []struct {
		zone      string
		wantUntil int
	}{
		{zone: "Pacific/Kiritimati", wantUntil: 0}, // local date Mar 15: birthday is today
		{zone: "America/New_York", wantUntil: 1},   // local date Mar 14: birthday is tomorrow
	}

	for _, z := range zones {
		t.Run(z.zone, func(t *testing.T) {
			setTimeNow(t, instant)

			db, _ := setupRouter()
			contact := models.Contact{UserID: seedUserID(t, db), Firstname: "Ann", Lastname: "Iv", Birthday: "--03-15", Archived: false}
			require.NoError(t, db.Create(&contact).Error)

			router := zoneRouter(t, db, z.zone)
			router.GET("/contacts/:id/briefing", GetContactBriefing)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/contacts/"+idString(contact.ID)+"/briefing", nil)
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)

			var briefing models.ContactBriefing
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &briefing))
			require.Len(t, briefing.UpcomingDates, 1)
			assert.Equal(t, "birthday", briefing.UpcomingDates[0].Label)
			assert.Equal(t, "--03-15", briefing.UpcomingDates[0].Date, "the stored value round-trips unchanged")
			assert.Equal(t, z.wantUntil, briefing.UpcomingDates[0].DaysUntil, "days-until must be computed against the %s calendar day", z.zone)
		})
	}
}

// seedUserID returns the id of the user the shared controllers test helper
// seeds.
func seedUserID(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	var user models.User
	require.NoError(t, db.First(&user).Error)
	return user.ID
}
