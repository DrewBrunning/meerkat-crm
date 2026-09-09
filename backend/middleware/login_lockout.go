package middleware

import "time"

// login_lockout.go is the issue #867 layer over AccountRateLimiter: the login
// call sites (controllers.LoginUser, controllers.Complete2FALogin,
// carddav.BasicAuthMiddleware) key their lockout on the (identifier, source-IP)
// pair rather than the identifier alone.
//
// Why: the old identifier-only lockout could be weaponised. Anyone who knew a
// victim's username (enumerable via /users/directory, or — before #862 — a
// timing oracle) could keep any account, including the sole admin,
// continuously locked out by replaying five wrong passwords per window; the
// legitimate user was denied by the same counter and there is no admin-unlock
// control. Keying on (identifier, IP) means a lockout only ever denies the
// source that caused it. A global per-identifier backstop still covers an
// attacker who rotates source IPs, and IPs that have recently authenticated
// for the account are exempt from that backstop so the real user is never
// caught by it.

// loginKeySep separates the two halves of a per-pair lockout key. NUL can't
// appear in a normalised identifier or a textual IP, so it can't be used to
// forge a collision across the boundary.
const loginKeySep = "\x00"

// LoginKey builds the per-(identifier, source-IP) lockout bucket key.
func LoginKey(identifier, ip string) string {
	return identifier + loginKeySep + ip
}

// IsLoginLocked reports whether a login attempt for identifier from ip must be
// refused, and for how many more seconds. It is refused when either the tight
// per-(identifier, IP) lock is active, or the global per-identifier backstop is
// active AND ip has not recently authenticated for this identifier. The
// returned seconds is the longer of whichever locks apply.
func (a *AccountRateLimiter) IsLoginLocked(identifier, ip string) (bool, int) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	now := time.Now()
	locked, secs := lockedForLocked(a.accounts, LoginKey(identifier, ip), now)

	if gLocked, gSecs := lockedForLocked(a.global, identifier, now); gLocked && !a.knownGoodIPLocked(identifier, ip, now) {
		locked = true
		if gSecs > secs {
			secs = gSecs
		}
	}

	return locked, secs
}

// RecordLoginFailure records a failed login for (identifier, ip): against the
// tight per-pair bucket (exponential backoff, like every other failed attempt)
// and against the global per-identifier bucket (a fixed-duration lock once
// GlobalAccountLoginAttempts is reached). Returns whether this ip is now locked
// out of this identifier and the retry-after seconds.
func (a *AccountRateLimiter) RecordLoginFailure(identifier, ip string) (bool, int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	pairLocked, pairSecs := recordExpBackoffLocked(a.accounts, LoginKey(identifier, ip), now)
	a.recordGlobalFailureLocked(identifier, now)

	locked, secs := pairLocked, pairSecs
	if gLocked, gSecs := lockedForLocked(a.global, identifier, now); gLocked && !a.knownGoodIPLocked(identifier, ip, now) {
		locked = true
		if gSecs > secs {
			secs = gSecs
		}
	}
	return locked, secs
}

// RecordLoginSuccess clears the caller's own per-pair bucket, resets the global
// per-identifier counter, and records ip as known-good for this identifier so a
// later backstop lock can't deny it. Other IPs' per-pair locks are deliberately
// left intact — an attacker's lock should keep denying the attacker.
func (a *AccountRateLimiter) RecordLoginSuccess(identifier, ip string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.accounts, LoginKey(identifier, ip))
	delete(a.global, identifier)

	ips := a.knownGoodIPs[identifier]
	if ips == nil {
		ips = make(map[string]time.Time)
		a.knownGoodIPs[identifier] = ips
	}
	ips[ip] = time.Now()
}

// recordGlobalFailureLocked bumps the per-identifier failure count across all
// source IPs and, at or over GlobalAccountLoginAttempts, sets a FIXED-duration
// lock (not exponential — the backstop's DoS amplification must stay bounded).
// Caller holds a.mu (write).
func (a *AccountRateLimiter) recordGlobalFailureLocked(identifier string, now time.Time) {
	entry, exists := a.global[identifier]
	if !exists {
		entry = &AccountLockoutEntry{LastAttempt: now}
		a.global[identifier] = entry
	}
	entry.FailedAttempts++
	entry.LastAttempt = now

	if entry.FailedAttempts >= GlobalAccountLoginAttempts && entry.LockedUntil.Before(now) {
		entry.LockedUntil = now.Add(GlobalAccountLockoutDuration)
	}
}

// knownGoodIPLocked reports whether ip authenticated for identifier within
// KnownGoodIPTTL. Entries past the TTL are treated as absent even before the
// periodic cleanup removes them. Caller holds a.mu (read or write).
func (a *AccountRateLimiter) knownGoodIPLocked(identifier, ip string, now time.Time) bool {
	seen, ok := a.knownGoodIPs[identifier][ip]
	return ok && now.Sub(seen) <= KnownGoodIPTTL
}

// GlobalEntryCount / KnownGoodIPCount expose the auxiliary maps for tests and
// monitoring, mirroring EntryCount.
func (a *AccountRateLimiter) GlobalEntryCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.global)
}

func (a *AccountRateLimiter) KnownGoodIPCount(identifier string) int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.knownGoodIPs[identifier])
}
