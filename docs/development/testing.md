---
title: Testing
parent: Development
nav_order: 4
---

# Test Pyramid

This page is the explicit test pyramid (issue #429, TEST-01): the answer to
"where does this test go?" that a reviewer can get **without asking**. Every
`0.6.x` milestone assumes a layer exists to hold its tests; this page names the
layers, the failure class each one owns, and how each maps onto CI path gating
(`.github/filters.yaml`, issue #264).

The rule of the page is one sentence:

> A test goes in the **lowest layer that can catch its failure class** — and a
> failure class with no owning layer is a **gap to file**, not a test to absorb
> into the nearest layer.

Coverage percentages are deliberately **not** the acceptance structure — see
the [anti-goal](#anti-goal-coverage-percentage-is-not-the-acceptance-criterion)
at the bottom. The diff-based coverage gate (issue #267) is a separate,
orthogonal mechanism; this page is about *which layer a test belongs to*, not
*how much of a change is covered*.

## The layers at a glance

| Layer | Responsible for | Must not be used for | Lives in | Runs via (local) | CI job / filter |
|---|---|---|---|---|---|
| Backend unit | Pure logic, no DB: parsing, temporal math, relationship-type inversion, cadence, validation | Persistence, GORM hooks, route wiring | `*_test.go` co-located per package | `go test ./...` | `backend-tests` legs + `backend-checks` / `backend` |
| DB/integration | Everything that touches the **real migrated schema**: hooks, flat-field derivation, delete semantics, ownership scoping, transactions, sync/backup/import services | Pure logic; JSON contract; format bytes | any `*_test.go` using `dbtest.New` or `database.InitDB(t.TempDir())` | `go test ./...` | `backend-tests` legs / `backend` |
| API contract | Route↔`openapi.yaml` drift, request-body binding, response DTO shapes, captured-response fixtures | Business logic, persistence, UI | `backend/openapi*_test.go`, `testdata/contract-fixtures/` | `go test ./...`, `npx vitest run`, `./gradlew testDebugUnitTest` | `openapi` (+ `schemathesis.yml`) |
| Import/export interop | vCard 3/4, JSContact, CSV, iCal/CalDAV against the RFC golden fixtures; round-trip semantics | Neutral-model logic, persistence | `backend/{vcard3,vcard4,jscontact,correspondence}/`, `internal/rfctest/`, `docs/golden-fixtures/` | `go test ./...` | `backend-tests`/`backend-checks` (fuzz) / `backend` |
| Migration | Schema evolution: up/down pairs, version/dirty tracking, data preservation, interrupted-migration recovery | Current-schema app behavior; deploy sequencing | `backend/database/migrations/`, `backend/database/migrate_*_test.go` | `go test ./...` | `backend-tests` (`rest` leg) / `backend` |
| Frontend unit | Component state/rendering, hooks, i18n key parity, contract-fixture parsing (vitest) | Real network, end-to-end flows | `frontend/src/**/*.test.ts(x)`, `frontend/viteConfig.test.ts` | `npx vitest run` + `npx tsc --noEmit` | `frontend` job / `frontend` |
| Android unit/Robolectric | View models, editors, screens, network parsing, offline/local-DB logic | Emulator/device flows, real backend | `android/**/src/test/` | `./gradlew testDebugUnitTest` | `test` job / `android` |
| E2E web | Complete user flows through the **shipped** artifact (image + compose + nginx + backend) | Anything reachable in a lower layer | `frontend/e2e/` | `npx playwright test` | `e2e` job / `frontend`+`openapi`+`infra` |
| E2E Android | Real app on emulator against the real backend; the Playwright analog | JVM-testable logic | `android/app/src/androidTest/` | `:app:connectedDebugAndroidTest` (see README-developer.md) | `android-e2e` job / `android`+`openapi`+`infra` |
| Release/install smoke | Clean install from nothing; misconfiguration diagnostics; startup ordering | Anything presupposing a working install | `backend/cmd/deploysmoke/`, `backend/config/startup_smoke_test.go`, `.github/workflows/deploy-smoke.yml` | `go test ./cmd/deploysmoke/... ./config/...`; `go run ./cmd/deploysmoke` against a fresh `docker compose up` | `deploy-smoke` job / `infra` |
| Performance/load | N+1/query-count regressions, benchmark bodies, concurrent-write smoke vs the deployed artifact, scale (planned) | Correctness (that's the pyramid) | `backend/**/benchmark` tests, `backend/cmd/loadsmoke` | `go test -bench . -benchtime=1x`, `go run ./cmd/loadsmoke` | `backend-checks` + e2e `loadsmoke` step |
| Security/adversarial | BOLA/IDOR, spec fuzzing, DAST, static analysis — the vulnerability classes no pyramid layer is shaped to catch | — | their own workflows | per-workflow | `schemathesis.yml`, `zap-dast.yml`, `codeql.yml`, `sast.yml`, … |

Detail and the hard-won traps for each layer follow.

## Backend layers

### Backend unit

- **Responsible for** pure computations with no database and no HTTP: vCard/CSV/iCal
  line encode and decode, date and timezone math, `RelationshipEdge.Type` inversion
  (`relationship_type_registry.go`), cadence and `Activity.Qualifying()` logic, name
  and sort-key computation, validation rules, i18n tokens.
- **Must not be used for** anything that touches the schema, GORM hooks /
  `BeforeSave` derivation, controller routing, or ownership scoping — those are
  DB/integration or contract concerns, and a unit test would silently miss them.
- **Lives in** `*_test.go` files co-located with each package
  (`models/`, `vcard3/`, `vcard4/`, `jscontact/`, `correspondence/`, `contactmodel/`).
- **Runs via** `go test ./...`. The same `go test` process also runs the
  DB/integration tests — the layer is a property of what the test *touches*, not
  which command runs it.

### DB/integration

- **Responsible for** everything that persists to or reads from the **real
  migrated schema**:
  - GORM hooks and `BeforeSave`/`BeforeCreate` derivation, and the
    `RecordForContact`-vs-`RecordFromContact` / `contact_card_merge.go` data-loss
    bug classes (CLAUDE.md backend traps #2 and #3);
  - soft- vs hard-delete semantics and the `DeleteContact`/`DeleteUser` cascade
    lists (trap #6/#7), including unique-index interactions with soft-deleted rows;
  - ownership scoping — zero IDOR holes (trap #5);
  - transactions, `SQLITE_BUSY` / `_txlock=immediate` behavior
    (`database/concurrent_write_test.go`, trap #9);
  - services that orchestrate multi-row operations: CardDAV/CalDAV sync,
    backup/restore (`database/backup_test.go`), import.
- **Must not be used for** pure logic (unit), the JSON wire contract (API
  contract), or format bytes (interop).
- **The non-negotiable fixture rule** (CLAUDE.md trap #1): test against the real
  migrated schema, never `AutoMigrate`. Prefer `internal/dbtest.New(t)`, which
  builds the migrated template once per test binary and hands each test an
  isolated copy (issue #632); the `database` package's own tests call
  `database.InitDB(filepath.Join(t.TempDir(), "x.db"))` directly because `dbtest`
  cannot import it (deliberate import cycle).
- **Lives in** any `*_test.go` that takes a `*gorm.DB` — `controllers/`,
  `services/`, `models/`, `database/`.
- **Runs via** `go test ./...`; CI `backend-tests` matrix (`controllers` /
  `services` / `rest` legs, gotestsum retry-on-failure + a separate no-rerun
  coverage pass).

### API contract

- **Responsible for** the boundary between the OpenAPI spec and the
  implementation:
  - route-vs-spec drift and schema presence (`backend/openapi_test.go`);
  - request-body binding shapes and whether validation tags actually run
    (`openapi_request_test.go`, issue #256);
  - response DTOs not silently dropping required keys — the `omitempty` + required
    TS field crash (CLAUDE.md frontend trap #8);
  - captured real responses, one canonical copy consumed by both web and Android
    parse tests: `testdata/contract-fixtures/` → `frontend/src/api/contractFixtures.test.ts`
    and `android/core/network/.../ContractFixtureTest.kt` (issue #257; the
    spec-derived shared fixture is issue #266);
  - property-based spec fuzzing of the running app (Schemathesis + `cmd/schemagate`,
    issue #369) and the deterministic cross-account BOLA sweep (`cmd/bolacheck`).
- **Must not be used for** business logic, persistence, or UI behavior.
- **Runs via** `go test ./...` (drift tests), `npx vitest run` + `./gradlew
  testDebugUnitTest` (fixture consumers), and the `schemathesis.yml` workflow
  (fuzz). The live counterpart is `frontend/e2e/apiContract.spec.ts`.

### Import/export interop

- **Responsible for** the formats crossing the boundary: vCard 3.0/4.0, JSContact,
  CSV, iCal/CalDAV.
  - **Directional correctness against the RFC-verbatim golden fixtures**
    (ADR-0003): `docs/golden-fixtures/` is the external test oracle; adapters
    copy fixtures into `backend/internal/rfctest/fixtures/` **unchanged** and
    tests assert against the RFC bytes. A green test that shares a misconception
    with its code proves nothing — the fixtures are what anchor the bytes.
  - **Round-trip semantics, not byte identity.** A round trip `canonical →
    format → canonical` must preserve *meaning*; repeated conversions must not
    progressively corrupt data. This is the milestone's
    "distinguish semantic equivalence from byte-for-byte representation."
  - **Differential testing against independent reference implementations**
    (TEST-08, issue #680): every corpus contact is also pushed through an
    implementation that shares no code with ours — Python vobject (vCard
    3.0/4.0), Rust calcard (JSContact), Go golang-ical (iCalendar) — and the
    round trip must stay semantically equivalent per TEST-03's comparator.
    This is the countermeasure to the round-trip suite's own blind spot
    (code and tests sharing one wrong reading of the spec). Known
    reference-side divergences are pinned per corpus entry with drift
    detection; a confirmed *our*-side bug is fixed, never pinned (it caught
    the CATEGORIES comma-list escaping bug and the RFC 9553 created/updated
    wire-shape bug, both fixed). Details: the "Differential suite" section
    below.
  - Lossy/unknown-field behavior, sensitivity filtering in the query, malformed
    input reject/ignore/preserve rules.
  - Hostile-input fuzzing seeded from the fixtures (issues #265/#376).
- **Must not be used for** the neutral model's own logic (unit) or persistence
  (DB/integration).
- **Runs via** `go test ./...`; fuzz targets run in `backend-checks` (short
  smoke on PR, 2m per target on the nightly schedule).

### Migration

- **Responsible for** schema evolution:
  - hand-written up/down pairs, append-only numbering, version tracking and the
    dirty flag (`schema_migrations`) — see `migrate_version_test.go`;
  - **data preservation** across migrations: real production data exists since
    `v0.2.0-alpha-candidate` (deployed 2026-08-04), so a rename/drop/retype needs
    a backfill, not a silent clean removal (`migrate_datapreservation_test.go`);
  - interrupted-migration behavior and failure diagnostics (v0.6.4, #436/#437/#438).
  - **Historical migration paths (MIG-01 #436 + MIG-02 #437, v0.6.4):** one
    schema-only dump per supported release (floor `v0.6.0`, issue #529),
    populated at test time from the TEST-02 manifest; the CI migration-tests
    workflow then matrixes one job per release through `database.InitDB`
    (`v0.6.0 → current` as the longest skip), round-trips every migration
    up → down → up against a populated fixture, and gates on every migration
    shipping its `.down.sql`. A release without a dump fails CI.
- **Must not be used for** current-schema application behavior (DB/integration)
  or deploy sequencing (release/install smoke).
- **Runs via** `go test ./...` (the `database` package lands in the `rest` leg)
  plus the `migration-tests.yml` matrix job (gated on the `backend` filter).

## Frontend layer

### Frontend unit (vitest)

- **Responsible for** component state and rendering, hooks/data-fetching logic,
  i18n key parity across all five locales (`src/i18n/locales.test.ts`),
  date-format providers, and contract-fixture parsing. Network is mocked; no
  browser.
- **Must not be used for** end-to-end flows (that's Playwright).
- **Gotchas that live here** (CLAUDE.md frontend traps): no auto-cleanup and no
  `globals: true` — add `afterEach(cleanup)` explicitly; MUI appends `" *"` to a
  required field's accessible label; don't nest `<Chip>` in `<Typography>`.
- **Runs via** `npx vitest run` + `npx tsc --noEmit`; CI `unit-tests.yml`
  `frontend` job (`yarn test:coverage`, type check, lint).

## Android layers

### Android unit / Robolectric

- **Responsible for** view models, editors (`MultiValueEditor`, `AddressEditor`),
  screens, network parsing (`ContractFixtureTest.kt`), and offline/local-DB logic,
  all on the JVM via Robolectric.
- **Must not be used for** real device/emulator flows (instrumented) or a real
  backend.
- **Runs via** `./gradlew testDebugUnitTest`; CI `android-tests.yml` `test` job
  (also runs detekt, `lintDebug`, `assembleDebug`, and the aggregated JaCoCo
  report).
- **Known gap, filed:** `SmsReceiver` is untestable under the current Hilt
  whole-app-graph test component — issue #327.

### E2E Android (instrumented)

- **Responsible for** the real app on an emulator/device against the **real**
  backend (`docker-compose.test.yml`): login → list → detail → edit → favorites →
  archive/delete + audit undo (issues #212, #238). This replaced the old manual
  Pixel 8a gate; runbook in `README-developer.md`.
- **Must not be used for** JVM-testable logic.
- **Runs via** `./gradlew :app:connectedDebugAndroidTest` (emulator), or
  `adb reverse tcp:7300 tcp:7300` + the same with
  `-Pandroid.testInstrumentationRunnerArguments.serverUrl=http://127.0.0.1:7300`
  on a physical device. CI `android-tests.yml` `android-e2e` job is deliberately
  **off the PR path** (issue #578, B5): push:main + nightly + manual, so an
  Android PR is gated by Robolectric only and the emulator signal lands
  post-merge and nightly.

## E2E web (Playwright)

- **Responsible for** complete user workflows through the **shipped artifact** —
  the all-in-one image (nginx + backend under supervisord) via
  `docker-compose.test.yml` on `localhost:7300`: register → login → create/edit
  contact → search → import/export → merge → favorites → archive → audit undo →
  backup/restore → admin/system-status → service worker → security headers →
  timeline endpoints. The only layer that exercises the whole deployed web stack,
  including the real first-boot migration path against a fresh DB.
- **Must not be used for** anything reachable cheaper in a lower layer.
- **Lives in** `frontend/e2e/` (specs, `fixtures.ts`, `global-setup.ts`).
- **Runs via** `npx playwright test`; CI `e2e-tests.yml` `e2e` job (builds the
  all-in-one image, starts compose, waits on `/health/live`, runs Playwright, then
  the `cmd/loadsmoke` concurrent-write pass against the running instance). The
  workflow also runs on the nightly schedule (un-path-gated, 03:25 UTC) so a
  backend-only change that breaks a shipped user flow is caught overnight — see
  [Layers vs CI path gating](#layers-vs-ci-path-gating).
- **Production-default pass (issue #274):** the main suite runs against
  `docker-compose.test.yml`, which raises the API rate limit and enables CalDAV
  so a full parallel run can finish — so the shipped defaults (real rate limit,
  CardDAV off) are never exercised unless a job asks for them. A second CI job
  (`e2e-prod-defaults`) boots the same image with
  `docker-compose.prod-defaults.yml` layered on top and runs only the
  `@prod-defaults`-tagged subset (auth, contacts, dashboard) single-worker,
  which keeps it comfortably under the production limit (burst 1000, 600ms
  refill). The override nulls the rate-limit vars to empty strings rather than
  pinning their values, so the run exercises the genuine `getIntEnv` default
  path and would catch a future change to the shipped default.

### Visual regression (issue #258)

`frontend/e2e/visual.spec.ts` snapshots a small, curated set of stable views —
the dashboard, the contacts list, a contact detail page and the "Add reminder"
dialog, at desktop (1280×720) and phone (390×844) widths. An unintended layout
or theme change to a pinned view fails the e2e job as a pixel diff. Baselines
are committed under `frontend/e2e/visual.spec.ts-snapshots/` and compared as
part of the normal Playwright run; the automatic per-test a11y scan
(`fixtures.ts`) also runs after each shot, so the views are pixel-identical
*and* axe-clean.

Regenerate baselines after an **intentional** visual change:

```sh
cd frontend
npx playwright test visual.spec.ts --update-snapshots
```

then review the regenerated images (the HTML report / `--ui` shows before/after
and the diff) before committing them. Snapshot files are suffixed
`-chromium-linux` — CI's platform, and the only suffix that should be committed.

The shots are made deterministic on purpose — see the spec header comment.
Dates are pinned with a frozen page clock, the app's primary font is injected
as a committed webfont (`e2e/fixtures/fonts/`, route-intercepted) so rendering
never depends on host fonts, and the dashboard/list responses are intercepted
with fixed payloads (the dashboard's "Stay in Touch" column is server-random).
Only add a view here that you are willing to regenerate when the design
changes.

### Service-worker upgrade suite (issue #476)

`playwright.sw.config.ts` runs a **separate** Playwright config (specs under
`e2e/sw-upgrade/`) that stages *service-worker upgrades* between two real
production builds. The main e2e stack serves a single fixed build on :7300 and
cannot express an upgrade; this suite replaces that server with its own harness
(`e2e/sw-upgrade/sw-server.mjs`) on the same port, which serves build "a", then
build "b", and lets each test flip the active build mid-test. The two builds
are generated by `e2e/sw-upgrade/fixtures.mjs` from the current source
(`vite build`, differing only in the entry-chunk label injected via
`MYCORRHIZAL_SW_FIXTURE` in `vite.config.ts` — enough to make
`service-worker.js` byte-different and the update real). No backend is needed:
these tests assert on the app shell, the worker lifecycle and `CacheStorage`.

It pins the lifecycle the milestone names "service-worker upgrades are tested"
and "old cached assets cannot permanently strand users":

- a deploy installs and **waits** behind an open tab (old build keeps serving,
  no mixed-version assets) until the update prompt's reload is clicked;
- activation cleans the previous build's precache entries (bounded, repeated
  rapid updates do not grow the cache);
- a user **offline through a deploy** converges on the new build on reconnect;
- the **escape hatch** (`/_recovery.html`, see
  [`service-worker-updates.md`](../service-worker-updates.md)) recovers a user
  stuck on a deliberately broken worker.

It runs on Chromium **and** Firefox. Run locally with
`npx playwright test -c playwright.sw.config.ts` (the webServer builds the
fixtures on first run); the `e2e-sw-upgrade` job in `e2e-tests.yml` runs it in
CI. `frontend-dev` is useless here (CLAUDE.md, T51): every build under test is
a real `vite build`.

## Release/install smoke (DEPLOY-01, issue #450)

- **Responsible for** a clean install that proves the **workflow**, not the boot.
  `.github/workflows/deploy-smoke.yml` starts from nothing (fresh checkout, no
  volumes, no DB, only the config `docs/getting-started.md` tells a new operator
  to set), brings the stack up via the documented `docker compose up -d --build`,
  then runs `backend/cmd/deploysmoke` against it: register the first user, log in,
  create a contact with several field types, relate it to a second contact,
  attach a file, upload a profile photo, search, export, and read every field
  back — each step touching a different subsystem a fresh install gets wrong
  (empty-DB migration, FTS index + triggers, attachment/photo directory
  permissions, JWT signing).
- Also owns **startup ordering** — the job asserts from the boot log that config
  validation and the empty-DB migration run both precede the HTTP listener, and
  that the first `/health/ready` already reports `migrations: ok` (issue #421) —
  and the **misconfiguration cases** real operators hit: missing `JWT_SECRET_KEY`,
  relative `ATTACHMENTS_DIR`/`PROFILE_PHOTO_DIR`, empty/`*` `FRONTEND_URL`. Those
  are unit-tested per field in `backend/config`; asserted at the real-binary
  boundary in `backend/config/startup_smoke_test.go` (builds `mycorrhizal`, runs
  it with a broken env, expects a non-zero exit whose output names the variable);
  and asserted again through the shipped container in the workflow for the vars
  the container entrypoint does not pre-empt (`JWT_SECRET_KEY`, `FRONTEND_URL` —
  a relative photo/attachment dir fails in `entrypoint.sh`'s chown first, so its
  validator message is a binary-boundary assertion only). A non-matching CORS
  `Origin` is also checked against the configured `FRONTEND_URL`.
- **Gates under `infra`** (Dockerfile, `docker-compose*`, `docker/`,
  `.env.example`, `backend/cmd/deploysmoke/**`) and runs nightly.
- `backend/config/startup_smoke_test.go` builds and runs the server binary, so it
  is skipped under `go test -short`.
- This is the layer that v0.6.6 (install/upgrade/backup/recovery) and its sibling
  DEPLOY-02 (#451, upgrade) / DEPLOY-03 (#452, interrupted startup) build on.

## Cross-cutting: performance/load and security

Two sets are **not** pyramid layers in the "write a test here" sense, but they
own failure classes the 0.6.x milestones name, so they get explicit homes.

### Performance/load

- **Owns** N+1 / query-count regressions (the `*_QueryCountIsBounded` tests,
  issue #261), benchmark bodies (`-bench . -benchtime=1x` in `backend-checks`),
  and concurrent-write stability against the **deployed** artifact
  (`cmd/loadsmoke` in the e2e job — the deployed counterpart to
  `database/concurrent_write_test.go`).
- **Owns** scale characterization and the large-dataset migration test — the
  PERF-01 basis (issue #468) and issue #495: `internal/largedata` scales the
  TEST-02 manifest (same shapes, more rows, pathological records at scale),
  `cmd/migratebench` measures migration duration / peak memory / peak disk per
  supported path, and the recorded numbers live in
  `docs/development/scale-testing.md`. The resource-exhaustion half is the
  chaos job's `large-migration-disk-full` (ENOSPC during a large migration
  fails closed) — see `docs/development/fault-injection.md`.

### Security/adversarial tooling

- **Owns** the vulnerability classes no layer above is shaped to catch: BOLA/IDOR
  (`cmd/bolacheck`, the deterministic complement to Schemathesis), spec-derived
  fuzzing for 5xx/auth (Schemathesis + `cmd/schemagate` + ignore list), DAST
  (ZAP, weekly), static analysis (CodeQL, golangci-lint/gosec, detekt, mobsfscan),
  dependency and workflow audits. These are **their own workflows** with their own
  triggers (PR/push/nightly per workflow); a feature PR writes tests in the
  pyramid, not in these. The security checklist (`docs/security/asvs-l2.md`) is
  the tracking surface for which control each one evidences.
- **Owns** the failure-injection / chaos scenarios — TEST-06, issue #434
  (v0.6.3): the in-process `faults` harness, the external-fault CI job
  (`chaos-tests.yml`), and the fault catalog
  (`docs/development/fault-injection.md`). It is the mechanism behind v0.8.0's
  adversarial audit; the domain milestones (MIG-04/05 #439/#440, DEPLOY-03
  #452, CON-04 #459, #526) consume it.

## Failure classes by owner

The `0.6.x` milestones name failure classes, not layers. Each one below is owned
by **exactly one** layer (or a filed gap issue); a class with no row here is a
gap to file, never something to silently absorb.

| Failure class | Owning layer | Milestone / issue |
|---|---|---|
| Pure-computation bugs: parsing, temporal/DST math, relationship-type inversion, cadence, validation | Backend unit | v0.6.11 |
| ORM-vs-migration schema drift, GORM column derivation | DB/integration | everywhere (trap #1) |
| Persistence bugs: silently swallowed `.Error`, flat-field derivation, `RecordForContact` vs `RecordFromContact`, delete/cascade semantics, soft-deleted unique-index collisions | DB/integration | v0.6.4, v0.6.8 (traps #2/#3/#6/#7) |
| IDOR / missing ownership scoping | DB/integration (+ `cmd/bolacheck` evidence) | v0.6.12 (trap #5) |
| Concurrent writes, `SQLITE_BUSY`, lost updates, stale writes, idempotency | DB/integration (`concurrent_write_test.go`) + E2E web (`loadsmoke`) | v0.6.7 |
| Route↔spec drift, binding-shape drift, dropped response keys | API contract | v0.6.3 (#266) |
| Client/server contract mismatch | API contract + E2E web/Android | v0.6.10 |
| vCard/JSContact/CSV/iCal format correctness, lossy conversions, unknown fields, malformed input, sensitivity filtering | Import/export interop | v0.6.5 |
| Round-trip fidelity, repeated-conversion stability | Import/export interop | v0.6.3 (TEST-03 #431, pulled forward from v0.6.5); v0.6.5 (DATA-03 #443 — idempotence-after-first-conversion) |
| Migration data loss, semantic drift across schema versions, dirty/version mishandling | Migration | v0.6.4 |
| Interrupted migration, migration rollback/recovery, first-boot migration on empty DB | Migration + release/install smoke | v0.6.4, v0.6.6, #438/#452 |
| Monica/Meerkat import mapping errors | Import/export interop (fixtures per DATA-*) | v0.6.4 (#351/#353) |
| Clean-install failure (env vars, CORS, permissions, empty-DB migration) | Release/install smoke (`deploy-smoke.yml` + `cmd/deploysmoke` + `config/startup_smoke_test.go`) | v0.6.6 (#450) |
| Backup/restore/cross-version restore failure | DB/integration (`backup_test.go`, restore tests) + release/install smoke | v0.6.6 |
| Component/hook state bugs, i18n key/placeholder drift, format-provider bugs | Frontend unit | v0.6.11 |
| Android view-model/editor/offline/local-migration bugs | Android unit/Robolectric | v0.6.7, v0.6.10 |
| Whole-user-flow breakage (register→create→search→export), shipped-artifact boot | E2E web | v0.6.3, v0.6.6 |
| Layout/theme regressions (a CSS refactor shifting a card or breaking a dialog) | E2E web (`visual.spec.ts`) | v0.6.3 (#258) |
| Service-worker lifecycle, stale-cache stranding | E2E web (`serviceWorker.spec.ts`) | v0.6.10 |
| Android real-app flows (favorites, archive/delete + undo) | E2E Android | #212/#238 |
| Referential integrity, orphan detection, reciprocal-relationship consistency, derived-data (FTS/flat-columns/cadence) drift | DB/integration (DB-01 checker, SEARCH-*) | v0.6.8 (#460/#461–463/#497) |
| External-integration failure behavior, retries | DB/integration (services-level) + failure injection (`docs/development/fault-injection.md`) | v0.6.9, #434 |
| Query/scale performance, resource exhaustion, N+1 | Performance/load | v0.6.9 (#468, #261) |
| Security vulnerabilities (BOLA, auth bypass, injection, SSRF) | Security/adversarial tooling | v0.6.12, v0.8.0 |
| Adversarial/chaos scenarios | Failure injection (TEST-06, #434 — in-process seams + external chaos job) | v0.8.0 |

## Differential suite (TEST-08, issue #680)

The round-trip suite (TEST-03) and the golden fixtures (ADR-0003) both compare
our output against oracles *we wrote*. The failure mode those cannot see is
code and tests consistently, confidently wrong — a parser and an exporter
sharing one (wrong) reading of the spec pass each other's round trips. The
differential suite runs the same corpus through an **independent, pinned
reference implementation** and requires semantic equivalence (TEST-03's
comparator), so a disagreement is evidence one of us misreads the RFC, and the
RFC is the tiebreaker.

**The reference must share no code with our adapters.** That rules out
`emersion/go-vcard` and `emersion/go-ical`, which our vcard3/vcard4 and caldav
packages are built on. The pinned references are:

| Format | Reference | Runs | Harness |
|---|---|---|---|
| vCard 3.0 / 4.0 | Python `vobject` 0.9.9 | per-PR (`unit-tests.yml`, pip-installed) + nightly | `backend/differential/reference/vobject/vcard_ref.py` |
| JSContact | Rust `calcard` 0.3.13 | scheduled + path-gated (`differential-e2e.yml`, built in a digest-pinned rust image) | `backend/differential/reference/calcard/` |
| iCalendar (CalDAV) | Go `github.com/arran4/golang-ical` (go.mod-pinned) | per-PR, no runtime deps | in-package tests in `caldav/` + `services/` |

Each vCard/JSContact leg runs both directions over the shared corpus (TEST-02
fixture + golden fixtures + pinned generated seeds): ours → reference (our
export, reference parses) and reference → ours (reference emits, our import),
asserting `semanticequal` equivalence per TEST-03. The iCalendar leg compares
VEVENT object fields (uid/summary/description/location/dtstart/rrule) by name.

**Divergence policy** (the #496 pattern): known reference-side divergences are
pinned per corpus entry + direction + concept with a written reason, and the
drift check fails when a pin stops reproducing (the reference got fixed) or an
unpinned disagreement appears. A confirmed *our*-side bug is **fixed, never
pinned** — the suite has already caught and fixed two:

- the vCard CATEGORIES comma-list escaping bug (`CATEGORIES:a\,b` meant "one
  category with a comma" to any spec-honoring reader, not two categories; our
  importer happened to split the unescaped value back), fixed in `vcard3`/
  `vcard4`; and
- the JSContact `created`/`updated` wire shape (RFC 9553 defines them as
  UTCDateTime **strings**; our adapter emitted a `@type`-discriminated
  object), fixed in `jscontact`.

**Supply-chain pins:** vobject is installed in CI from a hash-pinned
requirements file (`backend/differential/reference/vobject/requirements.txt`,
`--require-hashes` — zizmor audits pip installs for unpinned supply-chain
refs); calcard is version-pinned in `reference/calcard/Cargo.toml` (`=0.3.13`)
with a committed `Cargo.lock` for the transitive graph, and the build
toolchain is a digest-pinned rust image; golang-ical is pinned in
`backend/go.mod`.

**Running locally:** the vCard leg needs `python3` with `vobject` importable
(it skips with a message otherwise). The JSContact leg resolves
`$MYCORRHIZAL_CALCARD_CMD` (CI sets it to the built binary), then a prebuilt
`reference/calcard/target/{release,debug}/calcard-ref` (`cargo build --release
--locked`), then skips. The iCalendar leg is pure Go.

Where a row names a milestone but the concrete ticket lives elsewhere (e.g. the
"test infrastructure supports historical fixtures" acceptance criterion → MIG-01
#436, and "large/representative datasets" → TEST-02 #430), that ticket is the
implementer of the layer's capability, not a different layer.

## Real-server + real-client interoperability (TEST-09, issue #681)

TEST-08 (above) proves our *serialization* against reference parsers. TEST-09
proves our *servers* against real third-party implementations — the missing
half of the "code and tests consistently, confidently wrong" shape that #496
warned about. Two directions:

1. **Reference servers → our client** (the #496 breadth). The full sync
   lifecycle + TEST-02 fixture round trip runs against **real CardDAV servers
   in Docker**, selected by `MYCORRHIZAL_CARDDAV_SERVER_ID`:

   | Server | Container | Layout | Divergence register |
   |---|---|---|---|
   | Radicale | `tomsquest/docker-radicale` (digest-pinned) | `/<user>/contacts/` | vobject: `celine` rejected (multi-N ALTID), `bob` photo/ADR mangled |
   | Baikal | `ckulka/baikal` | `/dav.php/addressbooks/<user>/contacts/` | Sabre VObject serves vCard 3.0: the 4.0-only concepts land in passthrough (per-contact pinned) |
   | Nextcloud | `nextcloud:stable` | `/remote.php/dav/addressbooks/users/<user>/contacts/` | same as Baikal, plus `eve` rejected (BDAY re-validated as iCalendar datetime) |
   | DAViCal | — | — | not run: the only image (`janlo/davical`) is a legacy Debian-stretch build whose provisioning (SCRAM-incompatible libpq, no headless user/collection creation) is unpinnable. Covered structurally by the Sabre-family analysis via Baikal/Nextcloud. |

   The suite is `TestCardDAVReferenceServer_RoundTrip` in
   `backend/services/carddav_radicale_integration_test.go`, gated by
   `MYCORRHIZAL_CARDDAV_SERVER_URL` (skips otherwise). The workflow
   `carddav-e2e.yml` is a three-way matrix; each server is provisioned
   headlessly by `.github/scripts/carddav-reference/provision-{baikal,nextcloud}.sh`
   (Baikal has no headless setup story — the script drives its two-step web
   wizard over HTTP, normalizes the case-sensitive `dav_auth_type` YAML value,
   and seeds the DAV user/principal/address book directly in the SQLite DB).
   The divergence registers pin exactly what each server does to the fixture;
   anything else is a failure that names the server + concept.

2. **Reference client → our server** (the core deliverable). A **real
   vdirsyncer** (a mature CardDAV/CalDAV client with its own protocol
   implementation and its own vCard parser) consumes OUR server end-to-end:
   provision/discover, pull (asserting the client's stored bytes are
   semantically equal to the staged fixture — the pathological fixture
   round-trips through it), ETag-driven quiescence, and PUT-create/update/
   delete pushed by the client. `TestCardDAVVdirsyncer_ClientRoundTrip` in
   `backend/services/carddav_vdirsyncer_integration_test.go`, gated by
   `MYCORRHIZAL_OUR_CARDDAV_URL` (skips otherwise). The workflow
   `reference-clients-e2e.yml` builds our backend, starts it with CardDAV
   enabled, pip-installs vdirsyncer from a hash-pinned requirements file
   (`.github/scripts/carddav-reference/vdirsyncer-requirements.txt`, cp312
   wheels — regenerate per its header if setup-python moves), and runs the
   test, which registers its own throwaway user.

**Running locally:** start our server (see the launch.json note in CLAUDE.md),
then

```bash
cd backend
MYCORRHIZAL_OUR_CARDDAV_URL=http://127.0.0.1:8090 \
MYCORRHIZAL_VDIRSYNCER_CMD=/path/to/vdirsyncer \
  go test ./services/ -run '^TestCardDAVVdirsyncer_ClientRoundTrip$' -count=1 -v
```

For the server matrix, start one of the containers (the workflow scripts are
reusable: `provision-baikal.sh` / `provision-nextcloud.sh`), then

```bash
MYCORRHIZAL_CARDDAV_SERVER_ID=baikal \
MYCORRHIZAL_CARDDAV_SERVER_URL=http://127.0.0.1:5233 \
MYCORRHIZAL_CARDDAV_USER=syncuser MYCORRHIZAL_CARDDAV_PASSWORD=syncsecret \
  go test ./services/ -run '^TestCardDAVReferenceServer_RoundTrip$' -count=1 -v
```

**What the matrix has already caught and fixed** (all three were invisible to
the fake suite):

- **Filter-less `addressbook-query` returned an empty collection.** A query
  with no `<card:filter>` (RFC 6352 §8.6 — how Thunderbird, Apple Contacts,
  DAVx5 and vdirsyncer enumerate) was routed through go-webdav's `Filter`,
  whose empty `FilterAnyOf` matches nothing. Our own client's full-refetch
  fallback is a filter-less query, so our server used to answer our client
  with "no contacts". Fixed in `backend/carddav/backend.go` with a red
  handler-level test.
- **MKCOL for an unsupported second address book returned 500**, which clients
  read as "server broken" instead of the interop-correct "single address book
  by design" (403). Fixed with a red test.
- **GET/DELETE of a missing card returned 500** instead of 404 — vdirsyncer's
  delete round-trip surfaced it. Fixed with a red test.

**Known limitation (documented, not yet fixed):** our go-webdav client cannot
negotiate an `address-data` version (go-webdav v0.7.0 `AddressDataRequest` has
no version field), so against SabreDAV-based servers (Baikal/Nextcloud) the
full-refetch fallback would receive vCard 3.0 re-serializations; the pinned
divergence registers encode exactly that. Requesting vCard 4.0 from those
servers would collapse most of the register. Tracked as a follow-up.

**ETag hand-verification (per CLAUDE.md):** the matrix's quiescence and update
assertions ARE the automated ETag-correctness proof — vdirsyncer detects an
update only if our server's ETag changes on PUT, and the "unchanged server
makes no second pass" assertion fails if our server returns unstable ETags.
To hand-verify the reverse (reintroduce the historical `etag`-column mapping
bug — remove the `gorm:"column:etag"` tag on `ContactSyncLink.ETag` so GORM
writes `e_tag`): run `TestCardDAVReferenceServer_RoundTrip` against a real
server — the real migrated schema has no `e_tag` column, so the sync fails
loudly ("table contact_sync_links has no column named e_tag") instead of
silently carrying an empty `etag` column that would break incremental sync.
Restore the tag after confirming.

The **manual client matrix** (Apple Contacts macOS/iOS, Thunderbird,
Android native, DAVx5) — the clients that cannot be scripted — is a documented
checklist with last-run dates in `docs/development/reference-client-matrix.md`.

## Layers vs CI path gating

Each layer maps onto an existing area of `.github/filters.yaml` (issue #264) —
the single source of truth for path gating. **No new filter is required** — every
layer has a home, and a layer's tests are gated exactly when its filter fires —
with one deliberate exception: the `carddav` filter (issue #496), the
real-server (Radicale) complement to the fake-based CardDAV suite, which is too
slow and too flake-prone to run on every backend diff (same rationale as the
`chaos` filter, #434). The `changes` job's named outputs gate the suite jobs;
skipped suites report success, so all checks can be required in branch
protection.

| Layer | Filter area(s) in `.github/filters.yaml` |
|---|---|
| Backend unit | `backend` |
| DB/integration | `backend` |
| API contract | `openapi` (drift tests ride `backend`; fixture consumers ride `frontend`/`android`; spec fuzz + `cmd/schemagate` ride `openapi`) |
| Import/export interop | `backend` |
| CardDAV real-server interop | `carddav` (the fake-based CardDAV suite rides `backend` on every PR; the real-server Radicale job runs on the schedule + manual dispatch, and on a PR only when the `carddav` filter fires — issue #496) |
| Migration | `backend` |
| Frontend unit | `frontend` |
| Android unit/Robolectric | `android` |
| E2E web | `frontend` + `openapi` + `infra` (builds/runs the deployed artifact) + nightly full-suite run (schedule, 2026-08-28) |
| E2E Android | `android` + `openapi` + `infra` (push:main + nightly + manual only, #578) |
| Release/install smoke | `infra` (`deploy-smoke.yml` — clean-install workflow + startup ordering + misconfig cases; also nightly, #450) |
| Performance/load | `backend` (benchmarks) + `infra` (loadsmoke in the e2e job) |
| Security/adversarial | not a filter area — each workflow has its own triggers |

The `workflows` filter (`.github/**`) re-runs everything, because a workflow
change can affect any check. `docs/**` intentionally maps to nothing: a change
to this page triggers only the docs job — correct, because a documentation edit
cannot break a test layer.

Two nuances worth writing down, because the mapping is *almost* clean:

- `backend/openapi.yaml` matches **both** `backend` and `openapi`, so a contract
  change re-runs the Go suite and the spec-fuzz/contract suites, and the
  `frontend`/`android` jobs re-run their fixture-consuming tests. That is
  intended: the contract's home is `openapi`.
- The **web** E2E suite gates on `frontend`/`openapi`/`infra`/`workflows`, not
  `backend`: a pure `backend/**` change is covered by the Go checks in
  `unit-tests.yml` and does not re-run the full e2e stack on the PR. A
  backend-only change that breaks a shipped user flow is therefore caught by
  the **nightly** full web suite — `e2e-tests.yml` also triggers on the
  schedule event (03:25 UTC, un-path-gated since a scheduled run has no diff to
  gate on) — not by the PR. That is a deliberate cost/speed trade: the PR path
  skips the deployed-artifact stack for backend-only diffs, and the overnight
  run is where that signal lands.

## Anti-goal: coverage percentage is not the acceptance criterion

The milestone states it verbatim, and this project means it: **coverage is
measured where useful, but coverage percentage is not the primary acceptance
criterion.**

- **Acceptance is layer ownership.** A failure class is "covered" when its
  owning layer has a test that would catch it — not when a number crosses a
  threshold. "Where does this test go?" is answered by the layer map above;
  "is this failure class covered anywhere?" is answered by the ownership table.
- **The coverage gate is a different mechanism.** Issue #267's `codecov/patch`
  gate (target 100% on *new uncovered lines in changed files*, per area: Go /
  vitest / JaCoCo) is a gate on **changes**, not an acceptance criterion for
  milestones. The project-wide `codecov/project` number is deliberately
  informational. Full mechanics: `docs/development/coverage.md`.
- **Coverage ≠ correctness.** A line can be 100% covered while every assertion
  misses what a planted bug changes — that is exactly what mutation testing
  (`stryker.yml`, nightly, report-only on the core domain modules) measures, and
  why it complements rather than replaces the layers.

## Determinism and isolation (the "test infrastructure" acceptance criteria)

The layers above presuppose tests that are deterministic and isolated from
developer-specific state:

- **Real schema, per test.** `internal/dbtest.New(t)` hands each test an
  isolated byte copy of the per-binary migrated template (issue #632); the
  `database` package uses `database.InitDB(filepath.Join(t.TempDir(), "x.db"))`.
  No test ever depends on a developer's working DB.
- **Fresh artifact per e2e run.** The Playwright, instrumented-Android, and
  Schemathesis suites boot `docker-compose.test.yml` (`down -v` teardown) — a
  throwaway DB, never production data.
- **Deterministic identities.** `global-setup.ts` seeds the `TEST_USER`; tests
  do not depend on local state or ordering (Playwright reuses a `storageState`
  captured in the `setup` project).

## Bug disposition: a test that catches a bug owns it

The layers this page describes are the gate for every milestone from v0.6.3
forward, so a bug they catch must not wait for its "natural" domain milestone
to be fixed. Standing policy (2026-08-28, recorded in the v0.6.3 milestone
description and gate #533):

- **File or fix it in the milestone where it was found.** Any bug a test layer
  reveals is fixed as part of that milestone, or filed as an issue in that
  milestone with an explicit disposition — never silently deferred to an
  arbitrary later milestone. The milestone that builds the tests is the
  milestone that owns the bugs they catch; that is the whole reason the layers
  land before the domains they validate.
- **A failing test in these layers blocks a milestone.** The gate #533 standing
  criterion is that these layers' tests are green on the merge commit of every
  milestone from here forward. A bug the tests catch keeps a milestone blocked
  until it is fixed — it does not get "exempted because it was found late."

## References

- Issue #429 (TEST-01) — this pyramid; #533 — the v0.6.3 milestone gate.
- Issue #264 — path gating; `.github/filters.yaml` is the source of truth.
- Issue #267 — the diff-based coverage gate; `docs/development/coverage.md`.
- ADR-0003 — golden fixtures as the external test oracle;
  `docs/golden-fixtures/`.
- TEST-02 (#430) — the canonical pathological dataset that the DB, migration,
  and interop layers will consume; MIG-01 (#436) — versioned migration fixtures.
- TEST-03 (#431) — the semantic round-trip comparison (pulled forward into
  v0.6.3 so TEST-07 #435 can consume it).
- TEST-08 (#680) — differential testing against independent, pinned reference
  implementations (vobject / calcard / golang-ical); the format-layer
  counterpart of #496's reference server. Divergence pins live in
  `backend/differential/pins_*.go`.
- DATA-03 (#443) — repeated-conversion / idempotence-after-first-conversion
  testing: each format's conversion run N times over the TEST-02 fixture
  (serialized form byte-identical from pass 2 onward, successive-pass diffs
  empty, passthrough byte-stable and never growing, diagnostics not
  accumulating), cross-format chains (vCard 4 → vCard 3 → vCard 4) converging
  rather than degrading, and the same property wired into the TEST-07
  generative suite. The single round trip proves one conversion is faithful;
  this proves conversions compose (the property CardDAV sync runs on).
- DEPLOY-01 (#450) — the release/install smoke layer; TEST-06 (#434) — failure
  injection; PERF-01 (#468) — performance at scale.
- Issue #257 — contract fixtures; #266 — the spec-derived contract fixture.
