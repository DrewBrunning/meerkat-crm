package services

import (
	"time"

	"mycorrhizal/models"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// TOTP single-use / anti-replay (issue #873, RFC 6238 §5.2).
//
// ValidateTOTP answers "is this code valid right now" but not "has this code
// already been spent", so a code lifted inside its ±1 step (~90 s) window
// could authenticate a second, independent session. Recovery codes are single-
// use by row deletion (ConsumeRecoveryCode); these two helpers give the TOTP
// path the same guarantee: ValidateTOTPStep reports which counter step a code
// belongs to, and BurnTOTPStep records it so a replay of that (or an older)
// step is rejected.
// ---------------------------------------------------------------------------

// totpStepSeconds is the RFC 6238 period ValidateTOTP uses. The counter step
// stored per user is unix_seconds / totpStepSeconds.
const totpStepSeconds = 30

// ValidateTOTPStep reports whether code is a valid RFC 6238 code for secret and,
// when it is, the counter step it matched. It accepts the same ±1 step window as
// ValidateTOTP (current step plus the one before and after) and uses the same
// library primitive underneath (hotp.ValidateCustom — constant-time compare,
// six SHA-1 digits), so the two agree on validity.
//
// The returned step is the single-use token: the caller persists it via
// BurnTOTPStep and rejects any later code whose step is not strictly greater.
// A non-TOTP input (e.g. a recovery code) matches nothing and returns ok=false,
// so callers fall through to the recovery-code path unchanged.
func ValidateTOTPStep(secret, code string) (step int64, ok bool) {
	if secret == "" || code == "" {
		return 0, false
	}
	opts := hotp.ValidateOpts{Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	current := time.Now().UTC().Unix() / totpStepSeconds
	// Oldest first, so a match is deterministic if (impossibly, for distinct
	// codes) more than one candidate step validated.
	for _, candidate := range []int64{current - 1, current, current + 1} {
		if candidate < 0 {
			continue // # pragma: no cover -- only reachable with a system clock set before 1970; the guard makes the uint64 conversion below provably non-negative (gosec G115)
		}
		valid, err := hotp.ValidateCustom(code, uint64(candidate), secret, opts)
		if err == nil && valid {
			return candidate, true
		}
	}
	return 0, false
}

// BurnTOTPStep records step as the newest TOTP counter consumed by userID and
// reports whether the burn succeeded — i.e. whether step was strictly greater
// than the user's stored totp_last_used_step (or that column was NULL). The
// compare and the write are one conditional UPDATE, so two concurrent logins
// presenting the same code cannot both win, exactly as ConsumeRecoveryCode's
// single atomic delete makes a recovery code single-use (RFC 6238 §5.2).
//
// A false return means "reject this code": either it is a replay of an already-
// spent step, or the UPDATE errored. Callers must not fall back to any other
// acceptance path on false — the code was a valid-but-spent TOTP, not a
// recovery code.
func BurnTOTPStep(db *gorm.DB, userID uint, step int64) bool {
	res := db.Model(&models.User{}).
		Where("id = ? AND (totp_last_used_step IS NULL OR totp_last_used_step < ?)", userID, step).
		Update("totp_last_used_step", step)
	return res.Error == nil && res.RowsAffected == 1
}
