---
title: Deprecation Policy
nav_order: 22
---

# Deprecation policy

**This is the canonical deprecation statement (MAINT-01, issue #490).** It
defines how something on a public surface is retired: what must be announced,
what must replace it, how long it keeps working, how an operator or client
finds out at runtime, and how the removal date is tracked so removals actually
happen instead of accumulating forever.

It sits next to two neighbors and does not duplicate them:

- [`breaking-change-policy.md`](breaking-change-policy.md) (MAINT-02, issue
  #491) **classifies** what counts as a breaking change. This page governs the
  **removal window** for anything that page classifies as breaking. Removal is
  a breaking change *and* follows this policy — both, not either.
- [`issue-classification.md`](issue-classification.md) (MAINT-03, issue #492) is
  where a deprecation-tracking issue gets its labels, milestone, and severity.

A deprecation is a promise with three parts — **an announcement, a
replacement, and a window** — and a removal is what happens at the end of it.
A deprecation with no replacement is a removal wearing a nicer word; a
"deprecation" removed in the same release it was announced in is just a
removal.

## What is covered

The same surfaces a third party can depend on that
[`breaking-change-policy.md`](breaking-change-policy.md) lists — stated here so
this page stands alone, not re-derived:

1. **The `/api/v1` REST contract** — endpoints, methods, parameters, request
   bodies, response shapes, status codes, and error bodies, as documented in
   `backend/openapi.yaml`.
2. **The database schema**, insofar as data must survive and third parties read
   it directly.
3. **Configuration variable names and semantics** — env var names, their
   meaning, and their defaults.
4. **CLI commands and flags** — `cmd/migrate`, the behavior-bearing `make`
   targets, the other `backend/cmd` entrypoints.
5. **Exported data formats** — vCard 3/4, JSContact, CSV, iCal/CalDAV output.
6. **Documented behavior** — anything the docs promise that is not a bug.

Internal Go packages, internal endpoints, and implementation details are
explicitly out of scope: they can change in any release with no deprecation.

## The window

**At least one minor release, and never less than 90 days — whichever is
longer.**

Both halves are load-bearing:

- **A release-count window alone can elapse in a fortnight.** Self-hosted
  operators upgrade on their own schedule; two minor releases can ship in three
  weeks. A calendar floor keeps the window meaningful.
- **A calendar window alone ignores the release cadence.** If 90 days passes
  but no release has carried the deprecation notice, nobody upgrading has seen
  it yet.

90 days is the review period this repo already uses in one place and reuses
here rather than inventing a second number — it is `cmd/depexceptions`'
`maxWindowDays` for the dependency-advisory exception ledger, and the default
cited by [`dependency-upgrade-policy.md`](dependency-upgrade-policy.md).

**Deprecated things keep working, unchanged, for the whole window.** A
deprecated API field that starts returning different data, a deprecated config
variable whose default flips, a deprecated flag that changes what it does —
each is a breaking change under MAINT-02, not a deprecation. The only
observable difference during the window is the runtime signal below.

## Runtime discoverability

**A deprecation must be discoverable at runtime, not only in release notes.**
Operators read logs when something breaks; they do not re-read changelogs for
releases they already installed. The mechanism, per surface:

### API

A deprecated endpoint, parameter, or response field carries, on every response
that touches it:

- **`Deprecation`** — [RFC 8594](https://www.rfc-editor.org/rfc/rfc8594). Value
  `true`, or an `@`-prefixed Unix timestamp of the deprecation date.
- **`Sunset`** — an HTTP-date, the **earliest** removal date (the end of the
  window). Never a date inside the window.
- **`Link`** with `rel="deprecation"` pointing at this page, and where a
  replacement endpoint exists, a second `Link` with `rel="successor-version"`.

The element is also marked **`deprecated: true`** in `backend/openapi.yaml`, so
the generated API reference and the OpenAPI baseline
(`backend/internal/apibaseline`) both record it.

`backend/middleware/deprecation.go` (`DeprecationHeaders`, registered in
`backend/main.go`, matching only `/api/v1` routes) emits these headers from an
in-code table. The table is empty today — nothing in `/api/v1` is deprecated —
and the first entry lands with the deprecation that needs it.

### Configuration

A deprecated environment variable produces a **startup `WARN` log naming the
variable and its replacement** — e.g.

```
WARN: OLD_VAR_NAME is deprecated (since v0.7.0); use NEW_VAR_NAME instead
```

The deprecated variable keeps working for the whole window; the warning is the
only change. `config.checkDeprecatedEnvVars` in `backend/config/config.go`
drives this from a table that is empty today.

### CLI

A deprecated command or flag prints a one-line notice to **stderr** on use,
naming the replacement, and still does what it did.

## The register

**[`deprecations.md`](deprecations.md) is the machine-readable list.** Every
deprecation in a covered surface has a row there *before it ships* — the row is
part of the same change that adds the runtime signal, not a follow-up. The row
format is documented in that file's header.

An unenforced policy is a document, so the register is enforced:

```bash
cd backend && go run ./cmd/deprecations
```

fails the build when:

- a row is malformed, has an unknown `surface` or `status`, or has an unparseable date;
- the window is shorter than 90 days (`not-before` minus `deprecated-on`);
- `earliest-removal` is not a strictly later **minor** version than `deprecated-in`;
- a `deprecated` row names no `replacement`;
- a **`removed`** row records a `removed-in` / `removed-on` earlier than its own
  `earliest-removal` / `not-before` — **something removed before its window
  expired**, which is the case this whole policy exists to prevent.

It prints an advisory (and still exits `0`) for each `deprecated` row whose
`not-before` has passed: *eligible for removal; still supported until
explicitly removed*. Lag is allowed — a removal is a deliberate act — but it is
surfaced, not silent.

`go run ./cmd/deprecations` runs in the `Security-doc citations` job of
`.github/workflows/unit-tests.yml`, beside the `citecheck` and `depexceptions`
freshness gates, so it executes on every backend PR and on the nightly
full-suite run — an over-window entry surfaces within a day even with no PR
activity to trip over it.

## Removal is a MAINT-02 breaking change

When the window has fully elapsed, removal follows
[`breaking-change-policy.md`](breaking-change-policy.md)'s process **in addition
to** this one:

1. the deprecation window here is complete (announced, replacement shipped, at
   least one minor release **and** 90 days elapsed);
2. a migration path — for data (a real backfill or an explicit, deliberate
   decision the data is safe to lose) and for clients (what they must change);
3. a release-note entry naming the removal, what it breaks, and what
   operators/clients must do;
4. explicit approval, with the PR stating "breaking change under MAINT-02".

The register row moves to `status: removed` with `removed-in` / `removed-on` in
the same change.

## The data bar is higher than the shape bar

Per CLAUDE.md's post-`v0.2.0-alpha-candidate` rule: a request shape can change
with notice, but **removing a column that holds user data** needs a real
backfill or an explicit, stated decision that the data is safe to lose —
regardless of version, and never justified by "pre-alpha". A deprecation window
is about giving people time to migrate off an interface; it does not by itself
make the underlying data safe to drop. A schema deprecation whose removal would
lose data follows the data-retention lifecycle
(`docs/security/data-retention-lifecycle.md`) so every copy is accounted for.

## Interaction with the `0.x` present

**The promise — nothing on a covered surface is removed without a completed
window — takes effect at `1.0.0`.** Until then, breaking changes (removals
included) remain allowed, exactly as CLAUDE.md says: this is pre-alpha
software, not a stability commitment.

What this policy changes *before* `1.0.0`:

- a removal in a covered surface is still **recorded** — a `deprecations.md` row
  (going straight to `status: removed` if there was no prior notice) and a
  release note;
- the runtime signals and the register operate now, so the mechanism is
  exercised and trusted before it is load-bearing;
- a deprecation that *is* announced pre-`1.0` still gets a real window and a
  real replacement — the policy is not "ignored until `1.0`", it is "the
  no-removal-without-a-window guarantee starts at `1.0`".

Stated plainly so the existence of this page is not read as a promise it does
not yet make.

## Retroactive audit

Covered surfaces reviewed on 2026-09-08 for anything already deprecated in fact
but not in writing:

| Surface | Method | Found |
|---|---|---|
| `/api/v1` | `backend/openapi.yaml` scanned for `deprecated:` | none |
| DB schema | `backend/database/migrations/` reviewed for retained-for-rollback-only columns | none third parties are told to read |
| Config | `backend/.env.example` + `backend/config/config.go` reviewed for renamed/aliased vars | none |
| CLI | `backend/cmd/*` + `Makefile` reviewed for retained aliases | none |
| Export formats | the three exporters reviewed for retained legacy output | none |

The register therefore starts empty.

**Deliberately out of scope:** `models.PreferenceCategoryMedia`
(`backend/models/preference.go`) carries a "legacy/deprecated" comment, but
`Preference.Category` is an **open classifier** by design — no `oneof`
validator, callers must degrade gracefully on an unknown value (same reasoning
as `LifeEvent.Type`). An open classifier value is not a closed contract enum,
so retiring one is not a contract-surface deprecation and needs no register
row. The comment is an internal note to contributors, and the value is retained
only because migration `000030`'s `down.sql` writes it back on rollback.
