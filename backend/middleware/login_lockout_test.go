package middleware

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	ipA      = "203.0.113.10"
	ipB      = "203.0.113.20"
	victim   = "victim@example.com"
	attacker = "203.0.113.99"
)

// freshLimiter returns an AccountRateLimiter isolated from the process-wide
// singleton, with a long TTL so nothing is swept mid-test.
func freshLimiter() *AccountRateLimiter { return NewAccountRateLimiter(time.Hour) }

func TestLoginKey(t *testing.T) {
	if !strings.Contains(LoginKey("a", "1.2.3.4"), "\x00") {
		t.Fatal("LoginKey must separate its halves with NUL")
	}
	// The separator prevents ("a", "1.2.3.4") from colliding with ("a.1", "2.3.4").
	if LoginKey("a", "1.2.3.4") == LoginKey("a.1", "2.3.4") {
		t.Error("LoginKey halves must not be ambiguous")
	}
}

// TestLoginLock_PerPairIsolation: locking one (identifier, IP) pair does not
// lock the same identifier from a different IP.
func TestLoginLock_PerPairIsolation(t *testing.T) {
	a := freshLimiter()
	for i := 0; i < MaxLoginAttempts; i++ {
		a.RecordLoginFailure(victim, ipA)
	}
	if locked, _ := a.IsLoginLocked(victim, ipA); !locked {
		t.Fatal("ipA should be locked after MaxLoginAttempts failures")
	}
	if locked, _ := a.IsLoginLocked(victim, ipB); locked {
		t.Error("ipB must NOT be locked — the lockout is per (identifier, IP)")
	}
}

// TestLoginLock_GriefingDoesNotLockVictim is the core issue #867 guarantee: an
// attacker who knows the victim's identifier and floods failures from their own
// IP cannot lock the victim out from the victim's IP.
func TestLoginLock_GriefingDoesNotLockVictim(t *testing.T) {
	a := freshLimiter()
	for i := 0; i < MaxLoginAttempts*4; i++ {
		a.RecordLoginFailure(victim, attacker)
	}
	if locked, _ := a.IsLoginLocked(victim, attacker); !locked {
		t.Fatal("the attacker's own IP should be locked")
	}
	if locked, secs := a.IsLoginLocked(victim, ipB); locked {
		t.Errorf("victim's IP must stay open under a single-source griefing flood (got locked, retry_after=%d)", secs)
	}
}

// TestLoginLock_SameIPStillLocks: per-source brute-force protection is intact —
// repeated failures from one IP still lock that IP after MaxLoginAttempts.
func TestLoginLock_SameIPStillLocks(t *testing.T) {
	a := freshLimiter()
	for i := 0; i < MaxLoginAttempts-1; i++ {
		if locked, _ := a.RecordLoginFailure(victim, ipA); locked {
			t.Fatalf("locked early at attempt %d", i+1)
		}
	}
	locked, secs := a.RecordLoginFailure(victim, ipA)
	if !locked || secs <= 0 {
		t.Fatalf("MaxLoginAttempts-th failure from one IP must lock it (locked=%v secs=%d)", locked, secs)
	}
}

// TestGlobalBackstop_TripsAcrossIPs: an attacker who rotates source IPs (one
// failure each, so no per-pair lock ever fires) is still stopped by the global
// per-identifier backstop once GlobalAccountLoginAttempts is reached — a fresh
// IP is then refused.
func TestGlobalBackstop_TripsAcrossIPs(t *testing.T) {
	a := freshLimiter()
	for i := 0; i < GlobalAccountLoginAttempts; i++ {
		a.RecordLoginFailure(victim, rotatingIP(i))
	}
	locked, secs := a.IsLoginLocked(victim, "198.51.100.1") // never seen before
	if !locked {
		t.Fatal("a fresh IP must be refused once the global backstop trips")
	}
	want := int(GlobalAccountLockoutDuration.Seconds())
	if secs < want-5 || secs > want {
		t.Errorf("global backstop retry_after = %d, want ~%d (fixed, not exponential)", secs, want)
	}
}

// TestGlobalBackstop_KnownGoodIPBypass: an IP that has authenticated
// successfully for the identifier within KnownGoodIPTTL is exempt from the
// global backstop, so the legitimate user is never denied by failures piled up
// from elsewhere.
func TestGlobalBackstop_KnownGoodIPBypass(t *testing.T) {
	a := freshLimiter()
	a.RecordLoginSuccess(victim, ipB) // ipB is now known-good for victim

	for i := 0; i < GlobalAccountLoginAttempts*2; i++ {
		a.RecordLoginFailure(victim, rotatingIP(i))
	}

	if locked, _ := a.IsLoginLocked(victim, ipB); locked {
		t.Error("known-good IP must bypass the global backstop")
	}
	if locked, _ := a.IsLoginLocked(victim, "198.51.100.2"); !locked {
		t.Error("a non-known-good fresh IP must still be refused")
	}
}

// TestGlobalBackstop_FixedNotExponential: re-tripping the backstop after it
// expires yields the same fixed duration, never a doubled one.
func TestGlobalBackstop_FixedNotExponential(t *testing.T) {
	a := freshLimiter()
	for i := 0; i < GlobalAccountLoginAttempts; i++ {
		a.RecordLoginFailure(victim, rotatingIP(i))
	}
	a.mu.Lock()
	a.global[victim].LockedUntil = time.Now().Add(-time.Second) // force-expire
	a.mu.Unlock()

	a.RecordLoginFailure(victim, "198.51.100.3")
	_, secs := a.IsLoginLocked(victim, "198.51.100.4")
	want := int(GlobalAccountLockoutDuration.Seconds())
	if secs < want-5 || secs > want {
		t.Errorf("re-tripped backstop retry_after = %d, want ~%d (must not grow)", secs, want)
	}
}

// TestRecordLoginSuccess_ResetsGlobalCounter: a successful login clears the
// global per-identifier counter, so an attacker's partial progress is wiped.
func TestRecordLoginSuccess_ResetsGlobalCounter(t *testing.T) {
	a := freshLimiter()
	for i := 0; i < GlobalAccountLoginAttempts-1; i++ { // one short of the lock
		a.RecordLoginFailure(victim, rotatingIP(i))
	}
	a.RecordLoginSuccess(victim, ipB)
	if a.GlobalEntryCount() != 0 {
		t.Fatalf("global counter not cleared on success: %d entries", a.GlobalEntryCount())
	}

	// It must now take a full fresh budget to trip the backstop again.
	for i := 0; i < GlobalAccountLoginAttempts-1; i++ {
		a.RecordLoginFailure(victim, rotatingIP(1000+i))
	}
	if locked, _ := a.IsLoginLocked(victim, "198.51.100.5"); locked {
		t.Error("backstop tripped with fewer than GlobalAccountLoginAttempts failures after a reset")
	}
}

// TestCleanup_PrunesGlobalAndKnownGoodIP: stale global entries and expired
// known-good IPs are removed by the periodic sweep.
func TestCleanup_PrunesGlobalAndKnownGoodIP(t *testing.T) {
	a := NewAccountRateLimiter(time.Millisecond)
	a.RecordLoginFailure(victim, ipA) // creates accounts + global entries
	a.RecordLoginSuccess("other@example.com", ipB)

	// Age the known-good entry past KnownGoodIPTTL.
	a.mu.Lock()
	a.knownGoodIPs["other@example.com"][ipB] = time.Now().Add(-KnownGoodIPTTL - time.Hour)
	a.mu.Unlock()

	time.Sleep(3 * time.Millisecond) // past the 1ms accounts/global TTL
	a.CleanupStaleAccountEntries()

	if a.GlobalEntryCount() != 0 {
		t.Errorf("stale global entry survived cleanup: %d", a.GlobalEntryCount())
	}
	if a.KnownGoodIPCount("other@example.com") != 0 {
		t.Errorf("expired known-good IP survived cleanup: %d", a.KnownGoodIPCount("other@example.com"))
	}
}

// TestIsLoginLocked_ReturnsLongerLock: when both a per-pair lock and the global
// backstop apply to the caller's IP, the reported retry_after is the longer one.
func TestIsLoginLocked_ReturnsLongerLock(t *testing.T) {
	a := freshLimiter()
	// Lock the (victim, ipA) pair — short (BaseLockoutDuration) — and count 5
	// toward the global budget.
	for i := 0; i < MaxLoginAttempts; i++ {
		a.RecordLoginFailure(victim, ipA)
	}
	// Reach the global budget from other IPs; ipA is not known-good.
	for i := 0; i < GlobalAccountLoginAttempts; i++ {
		a.RecordLoginFailure(victim, rotatingIP(i))
	}
	locked, secs := a.IsLoginLocked(victim, ipA)
	if !locked {
		t.Fatal("ipA must be locked (both per-pair and global apply)")
	}
	if secs <= int(BaseLockoutDuration.Seconds()) {
		t.Errorf("retry_after = %d, expected the longer global lock (~%d)", secs, int(GlobalAccountLockoutDuration.Seconds()))
	}
}

// rotatingIP yields a distinct source-address string per call index so a test
// can simulate an attacker cycling source IPs. The limiter treats IPs as opaque
// keys, so the exact form doesn't matter — only that each is unique.
func rotatingIP(i int) string { return "rot-" + strconv.Itoa(i) }
