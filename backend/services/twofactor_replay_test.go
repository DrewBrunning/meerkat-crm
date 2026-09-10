package services

import (
	"sync"
	"testing"
	"time"

	"mycorrhizal/internal/dbtest"
	"mycorrhizal/models"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateTOTPStep_WindowAndStep pins that ValidateTOTPStep accepts exactly
// the same ±1 step window as ValidateTOTP and reports the counter step the code
// belongs to — the value the single-use burn is keyed on (issue #873).
func TestValidateTOTPStep_WindowAndStep(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("alice@example.com")
	require.NoError(t, err)

	now := time.Now().UTC()
	currentStep := now.Unix() / totpStepSeconds

	cases := []struct {
		name     string
		when     time.Time
		wantOK   bool
		wantStep int64
	}{
		{"current step", now, true, currentStep},
		{"one step back", now.Add(-30 * time.Second), true, currentStep - 1},
		{"one step forward", now.Add(30 * time.Second), true, currentStep + 1},
		{"two steps back (outside window)", now.Add(-60 * time.Second), false, 0},
		{"two steps forward (outside window)", now.Add(60 * time.Second), false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := totp.GenerateCode(secret, tc.when)
			require.NoError(t, err)

			step, ok := ValidateTOTPStep(secret, code)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantStep, step)
			} else {
				assert.Zero(t, step)
			}
			// ValidateTOTP must agree on validity for the same input.
			assert.Equal(t, tc.wantOK, ValidateTOTP(secret, code))
		})
	}
}

func TestValidateTOTPStep_RejectsGarbageAndEmpty(t *testing.T) {
	secret, _, err := GenerateTOTPSecret("bob@example.com")
	require.NoError(t, err)

	for _, code := range []string{"", "000000", "12345", "not-a-code"} {
		step, ok := ValidateTOTPStep(secret, code)
		assert.False(t, ok, "code %q must not validate", code)
		assert.Zero(t, step)
	}
	step, ok := ValidateTOTPStep("", "000000")
	assert.False(t, ok)
	assert.Zero(t, step)
}

// TestBurnTOTPStep_SingleUse is the core anti-replay guarantee: the first burn
// of a step wins, and the same step — or any earlier one — is rejected
// afterwards, while a strictly later step still succeeds.
func TestBurnTOTPStep_SingleUse(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "burn-user", Email: "burn-user@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)

	// Nothing spent yet: any step wins.
	assert.True(t, BurnTOTPStep(db, user.ID, 100), "first use of step 100")

	// Replay of the same step is rejected.
	assert.False(t, BurnTOTPStep(db, user.ID, 100), "replay of step 100")

	// An older step is rejected.
	assert.False(t, BurnTOTPStep(db, user.ID, 99), "older step 99")

	// A strictly newer step is accepted and advances the marker.
	assert.True(t, BurnTOTPStep(db, user.ID, 101), "newer step 101")
	assert.False(t, BurnTOTPStep(db, user.ID, 101), "replay of step 101")

	var stored models.User
	require.NoError(t, db.First(&stored, user.ID).Error)
	require.NotNil(t, stored.TOTPLastUsedStep)
	assert.Equal(t, int64(101), *stored.TOTPLastUsedStep)
}

// TestBurnTOTPStep_ScopedPerUser: one user's burn does not block another's.
func TestBurnTOTPStep_ScopedPerUser(t *testing.T) {
	db := dbtest.New(t)
	a := models.User{Username: "burn-a", Email: "burn-a@example.com", Password: "x"}
	b := models.User{Username: "burn-b", Email: "burn-b@example.com", Password: "x"}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Create(&b).Error)

	assert.True(t, BurnTOTPStep(db, a.ID, 500))
	assert.False(t, BurnTOTPStep(db, a.ID, 500))
	// Same step, different user — unaffected.
	assert.True(t, BurnTOTPStep(db, b.ID, 500))
}

// TestBurnTOTPStep_ConcurrentSameStepOnlyOneWins pins that the compare-and-set
// is atomic: two logins racing with the same code cannot both be accepted
// (RFC 6238 §5.2, the concurrent-write case CLAUDE.md trap #9 covers for the DSN).
func TestBurnTOTPStep_ConcurrentSameStepOnlyOneWins(t *testing.T) {
	db := dbtest.New(t)
	user := models.User{Username: "burn-race", Email: "burn-race@example.com", Password: "x"}
	require.NoError(t, db.Create(&user).Error)

	const n = 8
	var wg sync.WaitGroup
	results := make([]bool, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i] = BurnTOTPStep(db, user.ID, 777)
		}(i)
	}
	wg.Wait()

	wins := 0
	for _, r := range results {
		if r {
			wins++
		}
	}
	assert.Equal(t, 1, wins, "exactly one concurrent burn of the same step may win")
}
