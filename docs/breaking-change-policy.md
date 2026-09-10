---
title: Breaking-Change Policy
nav_order: 15
---

# Breaking-change policy

**This is the canonical breaking-change statement (MAINT-02, issue #491).** It
defines what `/api/v1` promises after `1.0.0`, what counts as breaking that
promise, what explicitly does not, how a breaking change is versioned and
shipped, and who decides. The deprecation policy (MAINT-01, issue #490) governs
the *removal window* for anything this page classifies as breaking; this page
classifies.

A change is **additive** when it only extends what already exists, and
**breaking** when it removes, renames, narrows, or reinterprets something a
client could already rely on. This page exists so that distinction is decided
once, in writing, instead of per-PR.

## The `/api/v1` promise

**After `1.0.0`, within the `1.x` line, `/api/v1` does not remove, rename,
narrow, or change the meaning of any field, endpoint, parameter, status code,
configuration variable, CLI command, flag, or export-format element that
shipped in `1.0.0` or any later `1.x` release.**

That is the single sentence everything below elaborates. Additive change —
new fields, new endpoints, new parameters, new enum values, new formats — is
free and needs no ceremony. Everything else is a breaking change and follows
the versioning and process rules here.

## What is covered

The policy applies to the surfaces a third party can depend on:

1. **The `/api/v1` REST contract** — endpoints, HTTP methods, parameters
   (query/path/header/body), request bodies, response shapes, status codes,
   and error bodies, as documented in `backend/openapi.yaml`.
2. **The database schema** *insofar as data must survive* — migrations must
   preserve user data; a change that loses data is breaking regardless of
   version (CLAUDE.md: "breaking *data* is a different, higher bar").
3. **Configuration variable names and semantics** — env var names, their
   meaning, and their defaults.
4. **CLI commands and flags** — `cmd/migrate`, `make` targets that ship
   behavior, etc.
5. **Export formats** — vCard 3/4, JSContact, CSV, iCal/CalDAV output.
6. **Documented behavior** — anything the docs promise that is not a bug.
7. **The supported runtime matrix (issue #472)** and **the
   [client/server compatibility floor](client-compatibility-policy.md)**
   (ANDROID-01, issue #478) — raising a minimum is a breaking change.

Internal Go packages, internal endpoints, and implementation details are
explicitly out of scope.

## What counts as breaking

A change is breaking if it does any of the following to a covered surface:

- **Removes or renames** a field, endpoint, parameter, status code,
  configuration variable, CLI command, flag, or format element.
- **Narrows accepted input** — e.g. a field that accepted any string now
  validates it, a parameter that was optional becomes required, an enum
  shrinks, a free-form field becomes constrained.
- **Changes a response's type or meaning** — a field's JSON type changes
  (`string` → `int`), a documented value now means something different, a
  status code changes meaning.
- **Changes a default in a way that alters behavior** — a config default that
  flips a documented default on or off.
- **Raises a supported-version minimum** — the runtime matrix (issue #472) or
  the [client floor](client-compatibility-policy.md) (issue #478) moves up.
- **Loses user data** — any migration, export, or operation that destroys
  data is breaking, period (see the data bar below).
- **Breaks a documented behavior** — the docs said X; the new code does not-X
  without a bug being involved.

## What is explicitly not breaking

- **Adding an optional request field** — clients that never sent it are
  unaffected.
- **Adding a response field** — *provided clients tolerate unknown fields*.
  This is a **client requirement**, not a server grace: a client that rejects
  unknown response fields silently converts every additive change into a
  breaking one. Both the web and Android clients must ignore unknown response
  fields, asserted by test (this repo's web `contractFixtures.test.ts` and
  Android `ContractFixtureTest.kt` pin it).
- **Adding an endpoint** or a parameter.
- **Adding an enum value** — clients must treat unknown enum values as data to
  display, not as something to crash on. Where an enum is documented as
  closed, adding a value is still non-breaking under this rule; a client that
  switches on enums exhaustively must have a documented default branch.
- **Performance changes** — a query getting faster or slower changes no
  contract.
- **Bug fixes that bring behavior into line with documentation** — this is
  the one judgement call in the list. The maintainer makes it, and a change
  justified this way must cite the documentation it was conforming to in the
  release notes (and, where it changes observable behavior, in the PR). If no
  documentation said the old behavior was right, it was a bug, not a contract.

## Versioning

- **Within `1.x`, `/api/v1` does not break.** The floor statement above holds.
- A genuinely necessary break means **`2.0.0`, not a parallel `/api/v2`**.
  Maintaining two live API versions is a real cost for a project this size —
  two code paths, two test surfaces, two documentation trees, and an unbounded
  support commitment. The chosen policy is a single supported major version:
  when `2.0.0` ships, `1.x` is retired per the deprecation policy (MAINT-01,
  issue #490), not maintained in parallel.
- Pre-`1.0.0`, breaking changes remain **allowed** (CLAUDE.md's standing
  position: pre-alpha software). What this policy changes pre-`1.0.0` is that
  a break is *classified and recorded* — the OpenAPI baseline drift test flags
  it, the release notes must name it, and a data-lossing change still needs
  the explicit call-out regardless of version.

## Process

A breaking change (post-`1.0.0`) requires **all** of:

1. **The deprecation window from MAINT-01 (issue #490)** — announced, with a
   replacement, for at least one minor release and never less than the stated
   calendar period. Nothing is removed in the same release it is deprecated in.
2. **A migration path** — for data (a real backfill or an explicit, deliberate
   decision that the data is safe to lose) and for clients (what they must do
   to keep working).
3. **A release-note entry** naming the change as breaking, what it breaks, and
   what clients/operators must do.
4. **Explicit approval** — a deliberate sign-off, not merely a merged PR. The
   PR title and body must state "breaking change under MAINT-02" so the
   approval is on record.

## The data bar is higher than the shape bar

Per CLAUDE.md, breaking a request shape is ordinary pre-`1.0` practice;
breaking **data** is a different, higher bar. A change that loses, corrupts,
or silently reinterprets user data:

- requires a real backfill or an explicit, stated decision that the data is
  safe to lose, **regardless of version** (pre- or post-`1.0.0`);
- is never justified by "pre-alpha" — that argument covers request shapes,
  not someone's stored relationships, notes, and history;
- follows the data-retention/deletion lifecycle
  (`docs/security/data-retention-lifecycle.md`) so every copy of the affected
  data is accounted for.

## How a breaking change is caught

The OpenAPI contract is pinned by a **frozen baseline snapshot** generated
from `backend/openapi.yaml`:

- `cd backend && go run ./cmd/genapibaseline` regenerates
  `backend/internal/apibaseline/testdata/v1.json` from the spec.
- The drift test (`backend/internal/apibaseline/drift_test.go`) loads the
  committed baseline and the current spec and fails if the spec **removes,
  renames, or narrows** anything the baseline records — a PR that deletes an
  endpoint, drops a field, shrinks an enum, or makes a parameter required
  cannot merge unnoticed.
- A second test pins the baseline is **current**: a spec change without
  regenerating the baseline fails too. Additive changes do not *fail* the
  drift check (they are not breaks), but they still require regeneration so
  the snapshot keeps recording what the spec promises — otherwise a field
  added today and removed next month would slip past a stale baseline.

So every contract change ships with a baseline diff, and that diff is the
machine-readable record of whether the change was additive or breaking: a
purely additive regeneration reads as additions, and any removal in the diff
is the flagged break the release notes and approval process must cover.

## Interaction with the upgrade floor

The supported-upgrade floor (`docs/upgrade-compatibility.md`, issue #529) is
part of this policy's covered surfaces: **raising the floor** (moving the
minimum supported version from `v0.6.0` to something later) is a breaking
change under the "raises a supported-version minimum" category, so it requires
a major version and the process above. The floor's own post-`1.0` rule — any
`1.x` upgrade from any earlier `1.x`, and from the final `0.9.x` — is the
upgrade side of the same stability contract this page states for the API.
