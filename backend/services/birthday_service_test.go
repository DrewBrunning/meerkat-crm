package services

import (
	"mycorrhizal/models"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetUpcomingBirthdays_TruncatesAfterMaxResultsPastTwoWeeks is a
// regression test for the resultCount truncation loop in
// GetUpcomingBirthdays: once a birthday is both beyond the two-week window
// and past maxResults (5), the loop must stop growing resultCount for good
// (birthdays are sorted ascending by days-until, so nothing later in the
// slice would ever re-satisfy either condition). This exact loop was
// rewritten from an if/else chain to a switch during a lint cleanup pass;
// break inside a switch only exits the switch, not an enclosing for loop,
// so a naive conversion would have silently kept iterating instead of
// stopping — this test locks in the correct (labeled-break) behavior.
func TestGetUpcomingBirthdays_TruncatesAfterMaxResultsPastTwoWeeks(t *testing.T) {
	db, _ := setupRouter()

	user := models.User{Username: "birthdaytester", Password: "password123", Email: "bday@example.com"}
	require.NoError(t, db.Create(&user).Error)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 8 contacts, each 3 days apart: days-until = 5,8,11,...,26. The first
	// 5 (up to day 17, still >2 weeks for the later ones) exceed maxResults
	// only after the 2-week (14-day) window closes it off; construct so
	// exactly 6 contacts fall within 14 days (forcing resultCount past
	// maxResults via the "within two weeks" branch) and 2 fall beyond both
	// the window and maxResults, which must NOT be included.
	days := []int{2, 4, 6, 8, 10, 12, 20, 24} // 6 within 14 days, 2 well beyond
	for i, d := range days {
		bday := now.AddDate(0, 0, d)
		contact := models.Contact{
			UserID:    user.ID,
			Firstname: "Contact",
			Lastname:  string(rune('A' + i)),
			Birthday:  bday.Format("2006-01-02"),
			Archived:  false,
		}
		require.NoError(t, db.Create(&contact).Error)
	}

	birthdays, err := GetUpcomingBirthdays(db, user.ID, now)
	require.NoError(t, err)

	// All 6 within-two-weeks birthdays must be present; the 2 far-future
	// ones must be excluded (they're both past maxResults and past the
	// two-week window).
	assert.Len(t, birthdays, 6, "expected only the 6 within-two-week birthdays, got %d: %+v", len(birthdays), birthdays)
	for _, b := range birthdays {
		days := DaysUntilBirthday(b.Birthday, now)
		assert.LessOrEqual(t, days, 14, "no returned birthday should be more than 2 weeks out given maxResults was already exceeded")
	}
}

// TestGetUpcomingBirthdays_CapsAtMaxResultsWhenNoneWithinTwoWeeks asserts
// the maxResults=5 cap applies when nothing is within the two-week window.
func TestGetUpcomingBirthdays_CapsAtMaxResultsWhenNoneWithinTwoWeeks(t *testing.T) {
	db, _ := setupRouter()

	user := models.User{Username: "birthdaytester2", Password: "password123", Email: "bday2@example.com"}
	require.NoError(t, db.Create(&user).Error)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// 8 contacts, all well beyond the 2-week window (20+ days out).
	for i := 0; i < 8; i++ {
		bday := now.AddDate(0, 0, 20+i)
		contact := models.Contact{
			UserID:    user.ID,
			Firstname: "Contact",
			Lastname:  string(rune('A' + i)),
			Birthday:  bday.Format("2006-01-02"),
			Archived:  false,
		}
		require.NoError(t, db.Create(&contact).Error)
	}

	birthdays, err := GetUpcomingBirthdays(db, user.ID, now)
	require.NoError(t, err)
	assert.Len(t, birthdays, 5, "expected exactly maxResults=5 birthdays when none are within the two-week window")
}

// TestDaysUntilBirthday_ShortStringReturns999 covers the guard clause for
// birthday strings too short to contain a month/day (len < 7), e.g. empty
// string or malformed data.
func TestDaysUntilBirthday_ShortStringReturns999(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, 999, DaysUntilBirthday("", now))
	assert.Equal(t, 999, DaysUntilBirthday("1990", now))
	assert.Equal(t, 999, DaysUntilBirthday("--1-1", now)) // len 5, too short
}

// TestDaysUntilBirthday_UnparsableMonthOrDayReturns999 covers the parse-error
// branch: a birthday string long enough to be parsed but with an
// out-of-range month or day.
func TestDaysUntilBirthday_UnparsableMonthOrDayReturns999(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, 999, DaysUntilBirthday("1990-13-40", now), "month 13 and day 40 are both out of range")
	assert.Equal(t, 999, DaysUntilBirthday("1990-99-99", now), "month and day out of range")
	assert.Equal(t, 999, DaysUntilBirthday("--ab-cd", now), "non-numeric month/day")
}

// TestDaysUntilBirthday_DecemberToJanuaryBoundary is the explicit Dec 31 ->
// Jan 1 boundary case called out in docs/adrs/0003-golden-fixtures-external-test-oracle.md
// Phase 3b: today is Dec 31, birthday is Jan 1 (tomorrow). The birthday-this-
// year (Jan 1 of the *current* now.Year()) is necessarily "before" today
// (Dec 31 of that same year), so the function must wrap it forward into next
// year rather than leaving it hundreds of days in the past.
func TestDaysUntilBirthday_DecemberToJanuaryBoundary(t *testing.T) {
	today := time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

	// Full ISO date birthday.
	assert.Equal(t, 1, DaysUntilBirthday("2020-01-01", today), "Jan 1 birthday should be 1 day away when today is Dec 31")

	// Year-unknown (--MM-DD) birthday.
	assert.Equal(t, 1, DaysUntilBirthday("--01-01", today), "Jan 1 birthday (year-unknown format) should be 1 day away when today is Dec 31")
}

// TestDaysUntilBirthday_JanuaryFirstBirthdayToday covers the other side of
// the year boundary: today IS Jan 1 and the birthday is also Jan 1, so the
// function must report 0 (today), not wrap forward a full year.
func TestDaysUntilBirthday_JanuaryFirstBirthdayToday(t *testing.T) {
	today := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, 0, DaysUntilBirthday("2020-01-01", today))
	assert.Equal(t, 0, DaysUntilBirthday("--01-01", today))
}

// TestDaysUntilBirthday_ForwardLookingNotRecentPast documents the direction
// DaysUntilBirthday actually calculates in: it always returns days *until*
// the next occurrence, never "days since it last happened". Checking on
// Jan 2 for a birthday on Dec 31 does NOT report "1 day ago" (which a naive
// signed diff might); it reports the number of days until the *next*
// Dec 31, i.e. almost a full year away.
func TestDaysUntilBirthday_ForwardLookingNotRecentPast(t *testing.T) {
	today := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) // 2026 is not a leap year

	got := DaysUntilBirthday("2020-12-31", today)

	assert.Equal(t, 363, got, "should count forward to the next Dec 31 (365 - 2 days elapsed), not backward to the one that just passed")
	assert.Positive(t, got, "DaysUntilBirthday must never return a negative 'days ago' value")
}

// TestDaysUntilBirthday_LeapYearFeb29CheckedInNonLeapYear documents current
// behavior for a Feb 29 birthday when now.Year() is not a leap year: since
// birthdayThisYear is built with time.Date(now.Year(), Feb, 29, ...) and Go's
// time.Date normalizes out-of-range days, Feb 29 rolls forward to March 1 in
// non-leap years rather than clamping to Feb 28.
func TestDaysUntilBirthday_LeapYearFeb29CheckedInNonLeapYear(t *testing.T) {
	today := time.Date(2025, 2, 20, 0, 0, 0, 0, time.UTC) // 2025 is not a leap year

	got := DaysUntilBirthday("2000-02-29", today)

	assert.Equal(t, 9, got, "Feb 29 normalizes to Mar 1 in a non-leap year (time.Date day-overflow), giving 9 days from Feb 20")
}

// TestDaysUntilBirthday_LeapYearFeb29CheckedInLeapYear sanity-checks the
// Feb 29 birthday when now.Year() actually is a leap year, where no
// normalization is needed.
func TestDaysUntilBirthday_LeapYearFeb29CheckedInLeapYear(t *testing.T) {
	today := time.Date(2024, 2, 20, 0, 0, 0, 0, time.UTC) // 2024 is a leap year

	got := DaysUntilBirthday("2000-02-29", today)

	assert.Equal(t, 9, got)
}

// TestDaysUntilBirthday_LeapDayCelebratedTodayOnMarchFirstNonLeap pins the
// date-only "advance to the next real calendar day" rule from
// docs/adrs/0015-temporal-semantics.md: a stored 29-Feb birthday in a
// non-leap now.Year() is celebrated on 1 March, so on 1 March 2025 the count
// must be 0 ("today"), not ~364.
func TestDaysUntilBirthday_LeapDayCelebratedTodayOnMarchFirstNonLeap(t *testing.T) {
	today := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC) // 2025 is not a leap year

	assert.Equal(t, 0, DaysUntilBirthday("2000-02-29", today))
	assert.Equal(t, 0, DaysUntilBirthday("--02-29", today))
}

// TestGetUpcomingBirthdays_LeapDayBirthdayReachableThroughThePreselect covers
// the digest gap: GetUpcomingBirthdays fetches by the birthday's *stored*
// month, so a stored 29-Feb birthday (month 02) is fetched during February
// (announced as "tomorrow" from 28 Feb, since its non-leap celebration is
// 1 Mar) but its stored month is not in the query window on 1 March itself.
// The leap-day OR branch in GetUpcomingBirthdays must fetch it on that one
// date so the digest and birthdays list can report it "today".
func TestGetUpcomingBirthdays_LeapDayBirthdayReachableThroughThePreselect(t *testing.T) {
	db, _ := setupRouter()

	user := models.User{Username: "leapdaytester", Password: "password123", Email: "leap@example.com"}
	require.NoError(t, db.Create(&user).Error)

	for _, bday := range []string{"2000-02-29", "--02-29"} {
		contact := models.Contact{UserID: user.ID, Firstname: "Leapling", Lastname: bday, Birthday: bday, Archived: false}
		require.NoError(t, db.Create(&contact).Error)
	}

	t.Run("March 1 of a non-leap year reports today", func(t *testing.T) {
		now := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
		birthdays, err := GetUpcomingBirthdays(db, user.ID, now)
		require.NoError(t, err)
		require.Len(t, birthdays, 2, "both leap-day birthdays must be fetched on Mar 1 of a non-leap year")
		for _, b := range birthdays {
			assert.Equal(t, 0, DaysUntilBirthday(b.Birthday, now), "leap-day birthday must be 'today' on Mar 1 2025")
		}
	})

	t.Run("Feb 28 of a non-leap year reports tomorrow", func(t *testing.T) {
		now := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
		birthdays, err := GetUpcomingBirthdays(db, user.ID, now)
		require.NoError(t, err)
		require.Len(t, birthdays, 2, "both leap-day birthdays must be fetched during February")
		for _, b := range birthdays {
			assert.Equal(t, 1, DaysUntilBirthday(b.Birthday, now), "leap-day birthday must be 'tomorrow' on Feb 28 2025")
		}
	})

	t.Run("March 1 of a leap year does not surface the past occurrence", func(t *testing.T) {
		// 2024 is a leap year: the 29-Feb occurrence was Feb 29 2024, the day
		// before; the next one is ~a year out, so the row must NOT be in the
		// preselect at all (the leap OR is gated on a non-leap year).
		now := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
		birthdays, err := GetUpcomingBirthdays(db, user.ID, now)
		require.NoError(t, err)
		assert.Empty(t, birthdays, "a leap-day birthday the day after its leap-year occurrence is not upcoming")
	})
}

// --- DATE-02 (issue #483): pathological date battery -------------------------
//
// Every case here runs at an explicitly injected instant (never time.Now) so
// the suite passes on any calendar day. The rules asserted are the ones
// written down in docs/adrs/0015-temporal-semantics.md (Rule 6 = 29 February
// advances to 1 March in a non-leap year; Rule 2 = date-only values are never
// zone-converted; "next occurrence" wraps forward a year).

// TestDaysUntilBirthday_LeapDayFullMatrix exercises the whole 29-Feb
// neighbourhood for both the full and the year-less stored form: before, on,
// and after the occurrence in leap and non-leap years, plus the century
// non-leap year 2100.
func TestDaysUntilBirthday_LeapDayFullMatrix(t *testing.T) {
	for _, stored := range []string{"2000-02-29", "--02-29"} {
		stored := stored
		t.Run(stored, func(t *testing.T) {
			t.Run("Feb 28 of a leap year is one day before", func(t *testing.T) {
				now := time.Date(2024, 2, 28, 0, 0, 0, 0, time.UTC)
				assert.Equal(t, 1, DaysUntilBirthday(stored, now), "the real 29-Feb exists in 2024, so it is tomorrow from Feb 28")
			})
			t.Run("Feb 29 of a leap year is today", func(t *testing.T) {
				now := time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)
				assert.Equal(t, 0, DaysUntilBirthday(stored, now))
			})
			t.Run("Feb 28 of a non-leap year is one day before the Mar 1 celebration", func(t *testing.T) {
				now := time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)
				assert.Equal(t, 1, DaysUntilBirthday(stored, now), "2025 has no Feb 29; the celebration advances to Mar 1")
			})
			t.Run("Mar 1 of a non-leap year is the celebration day", func(t *testing.T) {
				now := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
				assert.Equal(t, 0, DaysUntilBirthday(stored, now))
			})
			t.Run("Mar 1 of a leap year is not the occurrence day", func(t *testing.T) {
				now := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
				// The 2024-02-29 occurrence was yesterday; the next one
				// (wrapped to 2025, an advance-to-Mar-1 year) is 365 days out.
				assert.Equal(t, 365, DaysUntilBirthday(stored, now))
			})
			t.Run("Mar 2 of a non-leap year points at the following Mar 1", func(t *testing.T) {
				now := time.Date(2025, 3, 2, 0, 0, 0, 0, time.UTC)
				assert.Equal(t, 364, DaysUntilBirthday(stored, now))
			})
			t.Run("century non-leap year 2100 celebrates on Mar 1", func(t *testing.T) {
				assert.Equal(t, 1, DaysUntilBirthday(stored, time.Date(2100, 2, 28, 0, 0, 0, 0, time.UTC)))
				assert.Equal(t, 0, DaysUntilBirthday(stored, time.Date(2100, 3, 1, 0, 0, 0, 0, time.UTC)))
			})
			t.Run("leap years divisible by 400 keep Feb 29", func(t *testing.T) {
				assert.Equal(t, 0, DaysUntilBirthday(stored, time.Date(2000, 2, 29, 0, 0, 0, 0, time.UTC)))
				assert.Equal(t, 1, DaysUntilBirthday(stored, time.Date(2000, 2, 28, 0, 0, 0, 0, time.UTC)))
			})
		})
	}
}

// TestDaysUntilBirthday_DayBeforeOnAfter marches a fixed birthday across the
// day-before / day-of / day-after boundaries — where off-by-one lives — for a
// mid-month date and the two year-boundary dates (31 Dec, 1 Jan).
func TestDaysUntilBirthday_DayBeforeOnAfter(t *testing.T) {
	t.Run("mid-month birthday", func(t *testing.T) {
		today := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		assert.Equal(t, 1, DaysUntilBirthday("1990-06-16", today))
		assert.Equal(t, 0, DaysUntilBirthday("1990-06-15", today))
		// Day after: the next occurrence is ~a year out (2026 is not a leap year).
		assert.Equal(t, 364, DaysUntilBirthday("1990-06-14", today))
	})

	t.Run("Jan 1 birthday across the year boundary", func(t *testing.T) {
		assert.Equal(t, 2, DaysUntilBirthday("--01-01", time.Date(2025, 12, 30, 0, 0, 0, 0, time.UTC)))
		assert.Equal(t, 1, DaysUntilBirthday("--01-01", time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)))
		assert.Equal(t, 0, DaysUntilBirthday("--01-01", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
		assert.Equal(t, 364, DaysUntilBirthday("--01-01", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)), "after Jan 1 the next occurrence is next Jan 1 (364 days, 2026 non-leap)")
	})

	t.Run("Dec 31 birthday across the year boundary", func(t *testing.T) {
		assert.Equal(t, 1, DaysUntilBirthday("1990-12-31", time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC)))
		assert.Equal(t, 0, DaysUntilBirthday("1990-12-31", time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)))
		assert.Equal(t, 364, DaysUntilBirthday("1990-12-31", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)), "checked on Jan 1 the next Dec 31 is 364 days out (2027 non-leap)")
	})
}

// TestDaysUntilBirthday_EpochFarPastFarFuture pins that only the stored
// month/day participates in the next-occurrence arithmetic: a birth year in the
// 1800s, the year 0000, or a far-future year like 9999 must not change the
// count (there is no zone conversion and no age arithmetic — DATE-01 Rule 3).
// The count is identical to a year-less --MM-DD with the same month/day.
func TestDaysUntilBirthday_EpochFarPastFarFuture(t *testing.T) {
	today := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	// Same month/day everywhere: "today".
	assert.Equal(t, 0, DaysUntilBirthday("1800-06-15", today), "a birth year in the 1800s does not change the count")
	assert.Equal(t, 0, DaysUntilBirthday("0000-06-15", today), "year 0000 is not special")
	assert.Equal(t, 0, DaysUntilBirthday("9999-06-15", today), "a far-future year does not change the count")
	assert.Equal(t, 0, DaysUntilBirthday("--06-15", today))

	// The count to the next occurrence of a year-less style month/day.
	assert.Equal(t, 1, DaysUntilBirthday("1800-06-16", today))
	assert.Equal(t, 364, DaysUntilBirthday("9999-06-14", today), "the stored 9999 year is ignored; only Jun 14 matters")

	// At the epoch (1970-01-01): counts to mid- and end-of-1970.
	epoch := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 165, DaysUntilBirthday("1800-06-15", epoch), "Jan 1 -> Jun 15 1970 is 165 days")
	assert.Equal(t, 364, DaysUntilBirthday("9999-12-31", epoch), "Jan 1 -> Dec 31 1970 is 364 days (1970 non-leap)")
}

// TestDaysUntilBirthday_AbsentAndGarbageNeverBecomeJan1YearZero asserts the
// empty / absent / malformed cases from DATE-01: nothing here may silently
// resolve to a fake "1 January year zero" (or to 0 / "today"). Malformed and
// too-short values keep the 999 sentinel; the one lexical-but-out-of-range
// month/day a hand-rolled parser could map wrongly (1990-99-99, --13-40) also
// lands on 999 because time.Parse range-checks the month/day substrings.
func TestDaysUntilBirthday_AbsentAndGarbageNeverBecomeJan1YearZero(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for name, bday := range map[string]string{
		"empty string":      "",
		"whitespace":        "   ",
		"year only":         "1990",
		"partial year":      "--03",
		"month 13 day 40":   "1990-13-40",
		"yearless month 13": "--13-40",
		"month 99":          "1990-99-99",
		"non numeric":       "--ab-cd",
		"trailing dash":     "1990-01-",
	} {
		assert.Equal(t, 999, DaysUntilBirthday(bday, now), "%q must keep the 999 sentinel, never become a date", name)
	}

	// Even on the most dangerous day of the year for a Jan-1 accident (Jan 1
	// itself), an empty/garbage birthday is NOT "today".
	for _, bday := range []string{"", "1990", "1990-99-99"} {
		assert.Equal(t, 999, DaysUntilBirthday(bday, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), "%q on Jan 1", bday)
	}

	// A real 0000-01-01 stored value computes to the genuine next Jan 1, not to
	// an automatic 0: checked mid-2026 the next occurrence is Jan 1 2027, 200
	// days out (Jun 15 2026 -> Jan 1 2027).
	assert.Equal(t, 200, DaysUntilBirthday("0000-01-01", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)))
}

// TestGetUpcomingBirthdays_ExcludesAbsentBirthdays pins that null and empty
// birthday values are filtered by the query (they must never surface as
// "1 January year zero" or a 999-sentinel row).
func TestGetUpcomingBirthdays_ExcludesAbsentBirthdays(t *testing.T) {
	db, _ := setupRouter()
	user := models.User{Username: "absent-bday-user", Password: "password123", Email: "absent@example.com"}
	require.NoError(t, db.Create(&user).Error)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.Contact{UserID: user.ID, Firstname: "NoBirthdayAtAll", Archived: false}).Error)
	require.NoError(t, db.Create(&models.Contact{UserID: user.ID, Firstname: "EmptyBirthday", Birthday: "", Archived: false}).Error)
	real := models.Contact{UserID: user.ID, Firstname: "Real", Birthday: "--01-05", Archived: false}
	require.NoError(t, db.Create(&real).Error)

	birthdays, err := GetUpcomingBirthdays(db, user.ID, now)
	require.NoError(t, err)
	require.Len(t, birthdays, 1, "only the real birthday may surface")
	assert.Equal(t, real.ID, birthdays[0].ContactID)
}

// TestDaysUntilBirthday_DSTTruncationRegression is the DATE-02 fix pin: the
// days-until count must be whole *calendar days* between two local midnights,
// never a truncation of absolute elapsed hours. Across a US spring-forward the
// two local midnights bracketing the transition are 23 absolute hours apart, so
// truncation reported a Mar 9 birthday as "today" on Mar 8 (0 instead of 1).
func TestDaysUntilBirthday_DSTTruncationRegression(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// Spring forward 2026: Sunday Mar 8, 02:00 EST -> 03:00 EDT.
	t.Run("birthday the day after the spring-forward is tomorrow, not today", func(t *testing.T) {
		transitionDay := time.Date(2026, 3, 8, 12, 0, 0, 0, ny) // EDT, transition already happened
		assert.Equal(t, 1, DaysUntilBirthday("2000-03-09", transitionDay), "Mar 8 -> Mar 9 spans the 23-hour transition day")
	})
	t.Run("two days out across the gap", func(t *testing.T) {
		before := time.Date(2026, 3, 7, 12, 0, 0, 0, ny) // EST, the day before the transition
		assert.Equal(t, 2, DaysUntilBirthday("2000-03-09", before))
	})
	t.Run("same-day stays today across the transition", func(t *testing.T) {
		transitionDay := time.Date(2026, 3, 8, 12, 0, 0, 0, ny)
		assert.Equal(t, 0, DaysUntilBirthday("2000-03-08", transitionDay))
	})
	t.Run("fall-back day does not regress", func(t *testing.T) {
		// Fall back 2026: Sunday Nov 1, 02:00 EDT -> 01:00 EST. A day across
		// the fold is 25 absolute hours, which truncation already handled; the
		// rounding fix must not change it.
		foldDay := time.Date(2026, 11, 1, 12, 0, 0, 0, ny) // EST
		assert.Equal(t, 1, DaysUntilBirthday("2000-11-02", foldDay))
		assert.Equal(t, 0, DaysUntilBirthday("2000-11-01", foldDay))
	})
}

// TestDaysUntilBirthday_ZoneDecidesTodayNotStoredValue pins DATE-01 Rule 2 /
// Rule 4 at the service boundary: the SAME instant, carried in two different
// zones, selects a different "today" calendar day, and the stored --MM-DD value
// is never shifted — only which day is "today" changes. This is the exact
// mechanism by which the digest and the birthdays list could disagree with each
// other before DATE-02 aligned every surface to the reminder zone.
func TestDaysUntilBirthday_ZoneDecidesTodayNotStoredValue(t *testing.T) {
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati") // UTC+14, no DST
	require.NoError(t, err)
	ny, err := time.LoadLocation("America/New_York") // UTC-4 in March (EDT)
	require.NoError(t, err)

	instant := time.Date(2026, 3, 14, 23, 30, 0, 0, time.UTC)

	// In New York it is still Mar 14 19:30, so the Mar 15 birthday is tomorrow.
	assert.Equal(t, 1, DaysUntilBirthday("--03-15", instant.In(ny)))
	// In Kiritimati the same instant is Mar 15 13:30, so it is today.
	assert.Equal(t, 0, DaysUntilBirthday("--03-15", instant.In(kiritimati)))
}

// TestGetUpcomingBirthdays_ZoneSelectsMembership is the DB-level sibling of
// TestDaysUntilBirthday_ZoneDecidesTodayNotStoredValue: a stored --03-14
// birthday is "today" (and therefore in the fetch window) in New York on the
// fixed instant, but already a year out (outside the window) in Kiritimati,
// where the same instant is Mar 15.
func TestGetUpcomingBirthdays_ZoneSelectsMembership(t *testing.T) {
	db, _ := setupRouter()
	user := models.User{Username: "zone-member-user", Password: "password123", Email: "zonemember@example.com"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&models.Contact{UserID: user.ID, Firstname: "Boundary", Birthday: "--03-14", Archived: false}).Error)

	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	require.NoError(t, err)
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	instant := time.Date(2026, 3, 14, 23, 30, 0, 0, time.UTC)

	t.Run("New York: Mar 14 is today, birthday is in the window", func(t *testing.T) {
		birthdays, err := GetUpcomingBirthdays(db, user.ID, instant.In(ny))
		require.NoError(t, err)
		require.Len(t, birthdays, 1)
		assert.Equal(t, 0, DaysUntilBirthday(birthdays[0].Birthday, instant.In(ny)))
		assert.Equal(t, "--03-14", birthdays[0].Birthday, "the stored value round-trips unchanged")
	})

	t.Run("Kiritimati: the same instant is Mar 15, birthday has wrapped out of the window", func(t *testing.T) {
		birthdays, err := GetUpcomingBirthdays(db, user.ID, instant.In(kiritimati))
		require.NoError(t, err)
		assert.Empty(t, birthdays)
	})
}
