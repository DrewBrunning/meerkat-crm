package services

import (
	"fmt"
	"mycorrhizal/models"
	"slices"
	"time"

	"gorm.io/gorm"
)

// GetUpcomingBirthdays fetches upcoming birthdays for a specific user
// Returns birthdays sorted by days until birthday, with smart limits
func GetUpcomingBirthdays(db *gorm.DB, userID uint, now time.Time) ([]models.Birthday, error) {
	currentDay := now.Format("02")
	currentMonth := now.Format("01")
	// Use first day of next month to avoid overflow when current day doesn't exist in next month
	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location()).Format("01")

	var birthdays []models.Birthday

	// The month/day window a stored birthday's next occurrence must fall into.
	// Two occurrences are reachable this way:
	//   - a stored month == the current month, with a stored day >= today
	//     (this month, from today on);
	//   - a stored month == the next month (the whole month, so the top-5
	//     fallback below has candidates when nothing is inside two weeks).
	// Birthday format is YYYY-MM-DD or --MM-DD (ISO 8601); month is at
	// position LENGTH-4 (2 chars), Day is at position LENGTH-1 (2 chars).
	monthWindow := db.Where("SUBSTR(birthday, LENGTH(birthday) - 4, 2) = ? AND SUBSTR(birthday, LENGTH(birthday) - 1, 2) >= ?", currentMonth, currentDay).
		Or("SUBSTR(birthday, LENGTH(birthday) - 4, 2) = ?", nextMonth)
	// Leap-day advance (docs/adrs/0015-temporal-semantics.md): a stored
	// 29-Feb birthday is celebrated on 1 March in a non-leap year. During
	// February its stored month (02) keeps it inside the window above, so it
	// is announced as "tomorrow" from 28 Feb — but on 1 March itself its
	// stored month is no longer 03 (the month being queried), so it would
	// never surface as "today". Fetch it explicitly on that one date; the
	// year being a leap year is excluded because then the 29-Feb occurrence
	// was yesterday and the next one is ~a year out, far outside any window.
	if now.Month() == time.March && now.Day() == 1 && !isLeapYear(now.Year()) {
		monthWindow = monthWindow.Or("SUBSTR(birthday, LENGTH(birthday) - 4, 2) = '02' AND SUBSTR(birthday, LENGTH(birthday) - 1, 2) = '29'")
	}

	var contacts []models.Contact
	contactQuery := db.Model(&models.Contact{}).
		Where("user_id = ?", userID).
		Where("archived = ?", false).
		Where("birthday IS NOT NULL AND birthday != ''").
		Where(monthWindow)

	if err := contactQuery.Find(&contacts).Error; err != nil {
		return nil, fmt.Errorf("failed to retrieve upcoming birthdays: %w", err)
	}

	// Convert contacts to Birthday DTOs
	for _, contact := range contacts {
		name := contact.Firstname
		if contact.Nickname != "" {
			name = contact.Nickname
		}
		if contact.Lastname != "" {
			name += " " + contact.Lastname
		}

		birthdays = append(birthdays, models.Birthday{
			Type:           "contact",
			Name:           name,
			Birthday:       contact.Birthday,
			PhotoThumbnail: contact.PhotoThumbnail,
			ContactID:      contact.ID,
		})
	}

	// Sort by days until birthday
	slices.SortFunc(birthdays, func(a, b models.Birthday) int {
		daysA := DaysUntilBirthday(a.Birthday, now)
		daysB := DaysUntilBirthday(b.Birthday, now)
		return daysA - daysB
	})

	// Apply limit: max 5, but include all birthdays within 2 weeks
	const maxResults = 5
	const twoWeeksDays = 14

	resultCount := 0
countLoop:
	for i, b := range birthdays {
		days := DaysUntilBirthday(b.Birthday, now)
		switch {
		case days <= twoWeeksDays:
			resultCount = i + 1
		case resultCount < maxResults:
			resultCount = i + 1
		default:
			break countLoop
		}
	}

	if resultCount < len(birthdays) {
		birthdays = birthdays[:resultCount]
	}

	return birthdays, nil
}

// isLeapYear reports whether year is a Gregorian leap year.
func isLeapYear(year int) bool {
	// Dec 31 of a leap year is day 366.
	return time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC).YearDay() == 366
}

// DaysUntilBirthday calculates the number of days until a birthday from a given date
// Birthday format is YYYY-MM-DD or --MM-DD (ISO 8601)
//
// Next-occurrence semantics (docs/adrs/0015-temporal-semantics.md): the
// birthday's month/day is placed in now's year and, if that instant is before
// today, wrapped forward a year. time.Date's day-overflow implements the
// leap-day advance rule — a stored 29-Feb in a non-leap now.Year() becomes
// 1 March, so the count is "days until the celebration (1 Mar)", and returns 0
// on 1 March itself. Always forward-looking (never negative); malformed or
// year-less-too-short strings return the 999 sentinel.
//
// Caveat (DATE-02 / issue #483 territory): the final "absolute hours / 24"
// truncation is not DST-safe across a spring-forward, where two local
// midnights are 23 absolute hours apart — cadence_service.go's
// calendarDaysBetween documents the rounding sibling for the same hazard.
// The day-boundary comparisons here all use now.Location().
func DaysUntilBirthday(birthday string, now time.Time) int {
	if len(birthday) < 7 {
		return 999
	}

	// Extract month and day from end of string (works for both YYYY-MM-DD and --MM-DD)
	// Month is at position len-5 to len-3, Day is at position len-2 to len
	length := len(birthday)
	month := birthday[length-5 : length-3]
	day := birthday[length-2 : length]

	d, err1 := time.Parse("02", day)
	m, err2 := time.Parse("01", month)
	if err1 != nil || err2 != nil {
		return 999
	}

	birthdayThisYear := time.Date(now.Year(), m.Month(), d.Day(), 0, 0, 0, 0, now.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if birthdayThisYear.Before(today) {
		birthdayThisYear = birthdayThisYear.AddDate(1, 0, 0)
	}

	return int(birthdayThisYear.Sub(today).Hours() / 24)
}
