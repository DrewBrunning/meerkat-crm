---
title: Client/Server Compatibility Policy
nav_order: 18
---

# Client/server compatibility policy

**This is the canonical client/server compatibility statement (ANDROID-01, issue
#478).** Mycorrhizal is self-hosted: the server is upgraded by its operator on
their schedule, and each client (the Android app, a browser tab) is updated by
its user on a different, independent schedule. A months-long gap between the
two is the normal case, not an edge case, so this page states what happens on
either side of that gap — once, in writing — instead of leaving it to whichever
error message a mismatched request happens to produce.

This page is the **policy**. It does not itself expose a version, block a
screen, or run a check — that is the mechanism, split by client because the
remedies differ:

- **Android (issue #528)** — the app cannot update itself, so an old build can
  strand a user indefinitely. The mechanism reads the contract this page
  defines and renders a blocking force-update screen when the client is below
  the server's floor.
- **Web (issue #475)** — a browser tab can be told to fetch a new build and
  reload. The mechanism is a service-worker update prompt and, as a backstop,
  a forced reload when the contract mismatches.

Both mechanisms enforce **this** policy rather than inventing their own
interpretation of it; this page also states what discovering their own
implementations diverging from it means: whichever one is wrong.

## The default posture

**An old client keeps working against a new server until the server
explicitly declares a floor. Backward compatibility is the norm; breaking it
is an event.**

Concretely: shipping a new server version never, by itself, breaks an
existing client. A client only stops working when the server has been
deliberately configured to declare that client's version unsupported (see
"Moving the floor" below) — and that declaration is itself a breaking change
under [breaking-change-policy.md](breaking-change-policy.md) (MAINT-02, issue
#491), not a side effect of a routine release.

This is what makes additive API change free: a new endpoint, a new response
field, a new enum value never requires raising the floor, because an old
client that never asked for the new thing is unaffected by it. Only a
genuinely breaking change (per MAINT-02's definition — removing, renaming,
narrowing, or reinterpreting something a client could already rely on) is a
candidate for moving the floor at all, and even then moving the floor is a
choice, not an automatic consequence: the server can usually keep an old
handler around, or version the one thing that changed, rather than stranding
every client below it.

## Moving the floor

Raising the minimum supported client version is already listed in
[breaking-change-policy.md](breaking-change-policy.md) as one of the ways a
change counts as breaking ("raises a supported-version minimum"). Concretely,
moving the floor requires **all** of:

1. **A genuinely breaking API change behind it.** The floor does not move
   because a release happened; it moves because something in that release
   removes, renames, narrows, or reinterprets a contract surface an older
   client depends on, and there is no way to keep serving that client (no
   feasible dual code path, no acceptable degraded behavior).
2. **A recorded rationale.** The PR or release note that raises the floor
   states which client versions stop working and *why* keeping them working
   was not feasible — the same bar as any other MAINT-02 breaking change, not
   a weaker one because "it's just Android."
3. **Treatment as a MAINT-02 event**, including the process
   [breaking-change-policy.md](breaking-change-policy.md#process) already
   requires: a deprecation window per
   [MAINT-01](https://github.com/DrewBrunning/mycorrhizal-crm/issues/490)
   (announced, with a replacement, for at least one minor release and never
   less than the stated calendar period), a migration path, a release-note
   entry naming the change as breaking, and explicit sign-off.
4. **An update to the supported matrix below**, in the same change that moves
   the floor — the matrix is worthless if it lags the actual policy.

Moving the floor strands users who cannot easily update (sideloading is real
friction, not a two-second reload) — it should be rare, and it should be
visible in the release notes as the specific thing it is, not buried in a
changelog line about an unrelated feature.

## Newer client, older server

The more common self-hosted shape is the reverse: a phone auto-updates itself
(or the user sideloads a newer build) while the server sits untouched on an
older release for months. **A newer client must degrade against an older
server — hide or disable the features that server does not have — rather
than erroring on them.**

This follows from the same additive-change guarantee, read from the other
side: if a new client feature depends on a server capability that an older
server never shipped, that capability's absence is not a crash-worthy
surprise, it's the expected state of a normal self-hosted deployment. The
client is responsible for checking what the server actually supports before
depending on it, the same way the server is responsible for tolerating
unknown fields from a client per
[breaking-change-policy.md](breaking-change-policy.md#what-is-explicitly-not-breaking).
A non-blocking notice that the server could be upgraded to unlock a feature
is appropriate; a fatal error or a permanently broken screen is not.

The concrete per-feature gating mechanism is issue #692's Android `ServerFeature`
registry (android/core/domain/.../compat/ServerFeature.kt) — a `minServerVersion`
per server-backed capability — applied against the same `/health` version, plus
the server-side floor enforcement at auth described in "Server-side floor
enforcement" below. The client has a hard baseline under which degradation is
not attempted at all:

- **A server below the app's v0.6.0 baseline is refused outright.** The whole
  authenticated surface expects the v0.6.0 API contract (the same floor as the
  backend's database migration floor), so instead of failing on every screen the
  app renders a blocking "server needs an upgrade" screen. This is deliberately
  *not* the three-state check's fail-open case: it only fires when /health is
  reachable and reports an old-but-parseable version, so an unreachable or
  unparseable server still fails open.
- **Between the baseline and the current release**, capabilities whose endpoints
  arrived after v0.6.0 are hidden when the connected server predates them (the
  per-feature `minServerVersion` floors in the registry), so a newer client
  degrades gracefully rather than offering actions that would 404.

## Server-side floor enforcement

The client-side force-update screen is the UX layer; the authoritative backstop
lives on the server (issue #692). A native client advertises its own
`versionName` as an `X-Client-Version` request header on every call to the API
server. When `MIN_CLIENT_VERSION` is configured, every session-minting route —
`POST /register`, `POST /login`, `POST /login/2fa`, and the device-grant
exchange `POST /auth/device/session` — refuses a client whose header is absent,
is not a strict `major.minor.patch`, or is below the floor, with
`403 CLIENT_NOT_SUPPORTED`, **before any credential work**: no password
comparison, no account-lockout accounting, no user lookup. A modified client
that skips its own client-side check cannot get past it. (OIDC login runs in the
system browser and carries no such header; it is outside this enforcement and
relies on the client-side gate.) With no floor configured the header is
advisory — logged by the request logger — and never rejects.

## How a client discovers the contract

`GET /health` is unauthenticated and already exposes the fields needed for
the easy half of this — a version string for bug reports
(`backend/controllers/health_controller.go`):

```json
{
  "status": "healthy",
  "version": "0.6.10",
  "commit": "abc1234",
  "build_date": "2026-08-01T00:00:00Z"
}
```

Issue #528 extends this with the two fields the compatibility check actually
needs, kept on the same unauthenticated, non-sensitive endpoint so both
clients can read it before login:

- **`min_client_version`** — the oldest client `versionName` the server still
  supports. Absent or unset means no floor has been declared (the default
  posture above): every released client version is compatible. Present only
  when a MAINT-02 floor-move has actually happened.
- **`api_contract_version`** — the API contract generation the server speaks
  (see "The API versioning promise" below). While the API is on `v1` this is
  always `"v1"`; it exists so a future `v2` can be announced on `/health`
  before any client is required to react to it.

One server-side contract serves both clients even though they act on it
differently: Android renders a blocking force-update screen when its
`BuildConfig.VERSION_NAME` is below `min_client_version` (issue #528); the web
client prompts a reload, or forces one on contract mismatch (issue #475). Both
read the same two fields; neither invents a parallel signal.

On the server these two fields are wired in as of the v0.6.10 mechanism:
`api_contract_version` is emitted by every `/health` as the constant `"v1"`
while the route table is on `/api/v1`, and `min_client_version` is read from
the `MIN_CLIENT_VERSION` environment variable — unset (the default) means the
field is absent entirely and no floor is declared. Setting it is the MAINT-02
event "Moving the floor" above describes, and a malformed value refuses to
boot rather than silently failing open on clients.

Both mechanisms must **fail open** on a network error or a malformed
response: an unreachable `/health` is a routine event (the server restarting,
a flaky connection) and must never itself brick a client into a permanent
force-update or reload loop.

**Android-specific prerequisite:** this comparison is only meaningful once
the APK's own version identifiers are trustworthy. Issue #527 (making the
release APK's `versionCode`/signature verification a mandatory gate) is a
dependency of #528, not of this policy — but the policy above assumes a
correct, monotonically increasing client version is available to compare
against `min_client_version`.

## The API versioning promise this rests on

Everything above assumes clients and servers can agree on what a given API
version guarantees. That promise is stated once, in
[breaking-change-policy.md](breaking-change-policy.md#the-apiv1-promise) (MAINT-02,
issue #491): after `1.0.0`, within the `1.x` line, `/api/v1` does not remove,
rename, narrow, or change the meaning of anything that shipped in `1.0.0` or a
later `1.x` release, and a genuinely necessary break means `2.0.0`, not a
parallel `/api/v2`. This page does not restate that promise — it is the thing
"moving the floor" above is measured against. [DOC-03](https://github.com/DrewBrunning/mycorrhizal-crm/issues/488)
(integration ownership) and MAINT-02 itself should cite this page for the
client-facing consequences of that promise rather than re-deriving them.

Pre-`1.0.0`, per CLAUDE.md's standing position, breaking changes remain
allowed and routine — but per "Moving the floor" above, raising the client
floor is *still* the more deliberate action of the two, because the remedy
cost (an operator or user having to act) does not go away just because the
API is pre-1.0. The floor should move rarely even pre-1.0; it is the version
number, not the floor-moving discipline, that pre-1.0 status relaxes.

## Supported client/server matrix

This is the operator-facing answer to "will my phone still work if I upgrade
the server?", published alongside the
[supported runtime matrix](development/supported-runtime-matrix.md) (issue
#472) rather than folded into it — the runtime matrix states what the server
itself requires to run; this table states what it requires of the clients
talking to it.

| Server version range | Minimum client version (`min_client_version`) | Notes |
|---|---|---|
| All releases through the current `v0.6.x` line | *(none declared)* | No floor has ever been raised. Every released Android build and every web client remain compatible with every released server version, per the default posture above. |

A row is added here **only** when a floor actually moves, in the same change
that moves it (see "Moving the floor," requirement 4). Until then this table
having a single "no floor declared" row is not a placeholder — it is a
faithful, actionable statement of the current, real policy: nothing has ever
required a client to update to keep working.

## How to verify this policy is being followed

- A server release that only adds endpoints/fields/enum values ships with
  `min_client_version` unchanged (or unset).
- A server release that raises `min_client_version` cites the specific
  breaking change behind it, follows the MAINT-01 deprecation window, and
  updates the matrix above in the same change.
- Issue #528's Android mechanism and issue #475's web mechanism each implement
  exactly the three states described here (compatible / client-too-old /
  server-too-old-for-client) — neither invents a fourth state or a different
  trigger. The Android force-update state additionally raises **before** a
  session exists (issue #692): once a below-floor server's URL is configured
  the gate appears without attempting an authentication the server would
  refuse. Issue #692's server-too-old gate (a reachable server below the
  app's v0.6.0 baseline) is not a fourth compatibility state — it is the
  baseline floor's refusal, and it is equally fail-open against an unreachable
  or unparseable /health.
- A newer client against an older server degrades the specific feature the
  server lacks rather than failing the whole session: per-feature surfaces
  whose `minServerVersion` exceeds the connected server's version are hidden,
  per the Android `ServerFeature` registry.
- With `MIN_CLIENT_VERSION` set, the server refuses below-floor clients at
  authentication (403 `CLIENT_NOT_SUPPORTED`) before any credential work —
  the authoritative backstop for a client that skips its own check.
- `/health` unreachable or returning a malformed body leaves both clients
  fully functional (fail open).

## Related

- [Breaking-change policy](breaking-change-policy.md) (MAINT-02, issue #491) —
  defines what counts as breaking, including "raises a supported-version
  minimum"; this page is the client-facing elaboration of that one bullet.
- [Supported runtime matrix](development/supported-runtime-matrix.md)
  (COMPAT-01, issue #472) — the server's own runtime floor; this page's
  matrix is the client-facing counterpart.
- Issue #490 (MAINT-01) — the deprecation window this page's floor-moving
  process borrows.
- Issue #528 — the Android mechanism that enforces this policy.
- Issue #475 (WEB-01) — the web mechanism that enforces this policy.
- Issue #527 — the Android `versionCode`/signature-verification prerequisite
  for #528.
- Issue #692 — the per-feature `minServerVersion` gating (Android `ServerFeature`)
  plus the server-side floor enforcement at auth that make "newer client
  degrades" and the force-update backstop concrete.
