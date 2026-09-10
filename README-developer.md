**Project Overview**
- Mycorrhizal CRM is a split Go backend and React frontend; the API sits under /api/v1 and serves a single-page app that manages contacts, activities, notes, reminders, and photos.
- The backend boots in [backend/main.go](backend/main.go) where config, database migrations, cron-style reminders, and the Gin router are wired together.
- The frontend lives in [frontend/src](frontend/src) with React 19, TypeScript, and MUI components backed by a typed API layer and custom hooks.

**Backend**
- Route definitions in [backend/routes/routes.go](backend/routes/routes.go) apply layered middleware: request IDs, structured logging, rate limiting, JWT auth, and JSON validation.
- Controllers (see [backend/controllers/contact_controller.go](backend/controllers/contact_controller.go)) expect `validated` payloads injected by [backend/middleware/validation.go](backend/middleware/validation.go); pull inputs from the context instead of decoding again.
- Custom errors from [backend/errors/errors.go](backend/errors/errors.go) plus [backend/middleware/middleware.go](backend/middleware/middleware.go) map failures to consistent JSON envelopes; prefer returning `*apperrors.AppError`.
- Database access is via GORM models in [backend/models](backend/models) with JSON arrays (contacts.circles) and manual cascade cleanup in delete flows; wrap multi-entity writes in transactions.
- Scheduled reminders run from [backend/services/reminder_service.go](backend/services/reminder_service.go) using gocron; honor `REMINDER_TIME` and Resend email toggles from [backend/config/config.go](backend/config/config.go).

**Frontend**
- All network calls go through [frontend/src/api/client.ts](frontend/src/api/client.ts) which enforces auth headers, request timeouts, and auto-logout on 401; reuse it for new endpoints.
- Resource-specific modules in [frontend/src/api](frontend/src/api) pair with hooks in [frontend/src/hooks](frontend/src/hooks); pages like [frontend/src/ContactsPage.tsx](frontend/src/ContactsPage.tsx) consume `{ data, loading, error, refetch }` contracts.
- Auth helpers in [frontend/src/auth.ts](frontend/src/auth.ts) persist JWTs in localStorage; frontend assumes `VITE_API_URL` when constructing base URLs.
- Styling blends global CSS (App.css/index.css) with MUI theming; photo uploads land in backend static storage under `static/photos`.

**Workflows**
- Source backend/my_environment.env to `.env` before running the server
- Start the backend with `go run main.go` (or `make dev`) from backend/ after `go mod tidy`; migrations are embedded in the binary and auto-run on boot. Use `make migrate-up` or cmd/migrate for manual control during development.
- Frontend uses Yarn: `yarn install` then `yarn start` from frontend/; the Vite dev server proxies nothing, so point `VITE_API_URL` at the backend in `.env`.
- Logs use zerolog via [backend/logger/logger.go](backend/logger/logger.go); set LOG_LEVEL and LOG_PRETTY for debugging, and rely on request IDs threaded through middleware.
- Rate limiting is IP-based via [backend/middleware/rate_limiter.go](backend/middleware/rate_limiter.go); respect separate auth/general buckets when adding endpoints.

**Docker (All-in-one Image)**
- The whole app ships as a single container built from the root [Dockerfile](Dockerfile): the React bundle and the Go backend served together by nginx (which proxies `/api` same-origin), managed by supervisord.
- Copy `.env.example` to `.env` and configure `JWT_SECRET_KEY`, `FRONTEND_URL`, and optionally `DATA_PATH`/`PHOTOS_PATH` for volume locations.
- Deploy using the pre-built image from GHCR: `docker compose up -d`. Set `IMAGE_TAG` in `.env` to pin a specific version (default: `latest`).
- Build and run locally instead: uncomment the `build: .` line in [docker-compose.yml](docker-compose.yml), then `docker compose up -d --build` (or plain `docker build -t mycorrhizal-crm .`).
- The frontend build stage is **yarn-only**: `frontend/yarn.lock` must be present in the build context (it is committed and not `.dockerignore`d). There is no `npm` fallback — `yarn install --frozen-lockfile` and `yarn build` run unconditionally — so a trimmed build context that omits the lockfile will fail at the install step. Same for the split [frontend/Dockerfile](frontend/Dockerfile).
- Container defaults (`PORT`, `SQLITE_DB_PATH`, `PROFILE_PHOTO_DIR`) are set in the root [Dockerfile](Dockerfile); override via `.env` if needed. `PORT` is the backend's internal bind port (8081) — nginx listens on 8080, which is what's actually exposed from the container.
- The frontend bundle is built with an empty `VITE_API_URL` so it calls the API on relative paths; nginx (see [docker/nginx.conf](docker/nginx.conf)) proxies `/api`, `/health`, and `/carddav` to the backend on `127.0.0.1:8081`.

**Metrics (Prometheus, issue #389)**
- `GET /metrics` exposes a Prometheus text exposition (0.0.4). It is **opt-in**: the route is registered only when `METRICS_TOKEN` is set (16+ chars), and every scrape must send `Authorization: Bearer <METRICS_TOKEN>`. No token → no route.
- Implemented without a new dependency — a small hand-rolled registry in [backend/metrics](backend/metrics); the endpoint handler is [backend/controllers/metrics_controller.go](backend/controllers/metrics_controller.go).
- Families: `http_requests_total` / `http_request_duration_seconds` / `http_requests_in_flight` (labelled by method + matched route *template* + status), `job_runs_total` / `job_duration_seconds`, `system_events_total` (sync / notification / backup / webhook / job outcomes, via `models.RecordSystemEvent`), `db_connections_*`, `go_*` / `process_*`, `mycorrhizal_build_info`, `mycorrhizal_storage_bytes` + `filesystem_{free,size}_bytes`. Labels are deliberately bounded — never a contact ID or a raw path.
- Quick check: `curl -s -H "Authorization: Bearer $METRICS_TOKEN" localhost:8080/metrics`.
- Example Prometheus scrape config:
  ```yaml
  scrape_configs:
    - job_name: mycorrhizal
      metrics_path: /metrics
      authorization:
        credentials: <METRICS_TOKEN>
      static_configs:
        - targets: ["mycorrhizal.example.com"]
  ```

**Testing**
- Backend Go tests (`go test ./...` or `make test`) spin up in-memory SQLite in helpers like [backend/controllers/activity_controller_test.go](backend/controllers/activity_controller_test.go); mirror that pattern for new suites.
- Validation and middleware behavior has dedicated coverage in [backend/middleware/validation_test.go](backend/middleware/validation_test.go) and related files—extend these before touching shared validators.
- Reminder scheduling is covered in [backend/services/reminder_service_test.go](backend/services/reminder_service_test.go) with clock control helpers; keep cron changes tested there.
- Frontend unit tests run with `yarn test` and rely on React Testing Library setup in [frontend/src/setupTests.ts](frontend/src/setupTests.ts), which already registers jest-dom.

**E2E Testing (Playwright)**
- End-to-end tests use Playwright against a real backend running in Docker; test files live in [frontend/e2e](frontend/e2e).
- Start the test stack: `docker compose -f docker-compose.test.yml up -d --build`
- Run tests: `cd frontend && npm run test:e2e` (or `test:e2e:ui` for interactive mode)
- Stop and clean up: `docker compose -f docker-compose.test.yml down -v`
- Tests run automatically in CI on push/PR to main via [.github/workflows/e2e-tests.yml](.github/workflows/e2e-tests.yml).
- **Production-default pass (issue #274):** `docker-compose.test.yml` deliberately raises the API rate limit and enables CalDAV so the full suite can finish — which means the shipped defaults are never exercised unless asked for. A second CI job (`e2e-prod-defaults`) boots the same image with [docker-compose.prod-defaults.yml](docker-compose.prod-defaults.yml) layered on top (restores the real rate limit, CardDAV off) and runs only the specs tagged `@prod-defaults` (auth, contacts, dashboard) single-worker, so the run stays under the production limit. Run it locally with:
  ```bash
  docker compose -f docker-compose.test.yml -f docker-compose.prod-defaults.yml up -d --build --wait
  cd frontend && npx playwright test --grep @prod-defaults --workers=1
  docker compose -f docker-compose.test.yml -f docker-compose.prod-defaults.yml down -v
  ```
- **Visual regression (issue #258):** `frontend/e2e/visual.spec.ts` snapshots the dashboard, contacts list, contact detail and the add-reminder dialog at desktop + phone widths and fails CI on any pixel diff. Baselines are committed under `frontend/e2e/visual.spec.ts-snapshots/`. Regenerate after an intentional visual change with `cd frontend && npx playwright test visual.spec.ts --update-snapshots`, then review the diff in the HTML report before committing. See the spec's header comment for why the shots are made deterministic (frozen clock, committed webfont, intercepted list/dashboard payloads).

**Android E2E (instrumented, issue #238)**
- The instrumented suite in [android/app/src/androidTest](android/app/src/androidTest) drives the *real* app (MainActivity + Hilt graph) against the *real* `docker-compose.test.yml` backend, on an emulator or a physical device — the Android counterpart to the web Playwright suite. It covers login → list → detail → edit → list refresh, the favorites flow (issue #212), archive/delete + the audit-undo round-trip, the FCM device-registration lifecycle (issue #481), and — `OfflineSyncE2eTest` (issue #479) — the offline outbox lifecycle: queue-while-offline → reconnect → exactly-one-server-record-per-entry, ambiguous-failure retry not duplicating (via the per-row `Idempotency-Key`), a stale contact link being dropped on reconnect (ADR-0009), cold-reopen durability of the encrypted mirror, a long offline period draining cleanly, and server-side deletions not leaving cache ghosts.
- Start the test stack: `docker compose -f docker-compose.test.yml up -d --build`
- **Emulator (one command):**
  ```bash
  cd android
  ./gradlew :app:connectedDebugAndroidTest
  ```
  The suite defaults to `http://10.0.2.2:7300` (the emulator's host-loopback alias), which the debug build's cleartext allowlist permits.
- **Physical device (Pixel 8a):** the device cannot reach `10.0.2.2`, so tunnel the host backend onto the device's own loopback and point the suite at it:
  ```bash
  adb reverse tcp:7300 tcp:7300
  cd android
  ./gradlew :app:connectedDebugAndroidTest \
    -Pandroid.testInstrumentationRunnerArguments.serverUrl=http://127.0.0.1:7300
  ```
  This is exactly what the CI job does. Any `serverUrl` instrumentation arg overrides the default.
- Notes:
  - The suite registers its own seed account (`e2euser`) and creates/cleans up its own `E2E *` contacts; it never touches user data. It needs registration enabled, which `docker-compose.test.yml` already sets.
  - The seed backend data is persistent — `docker compose -f docker-compose.test.yml down -v` resets it between runs if you want a clean slate.
  - CI runs this on every push/PR to main via the `android-e2e` job in [.github/workflows/android-tests.yml](.github/workflows/android-tests.yml).

**Android call/SMS tracking runbook (issue #721)**
- The "Log calls as activities" / "Log messages as activities" toggles in Settings (Tracking & notifications) are **opt-in and runtime-permission-gated**: flipping a toggle on requests the matching dangerous permissions (`READ_CALL_LOG` + `READ_PHONE_STATE` for calls; `READ_SMS` + `RECEIVE_SMS` for messages) through the system dialog. The opt-in flag is only stored once the grant lands.
  - Denied once → the toggle stays off and a rationale dialog offers "Try again".
  - Denied permanently ("don't ask again") → the dialog offers "Open settings", a deep link to the app's page in system settings.
  - The permissions were being **requested nowhere** before this issue — the bug report was literally true: nothing captured anything, because no runtime grant ever happened. The capture machinery itself (`CallLogSyncWorker`, `SmsReceiver`, etc.) was already in place; it was starved of permissions.
- What each toggle captures once granted:
  - **Calls**: every call is staged from the call log by `CallLogSyncWorker`, triggered live by `PhoneStateReceiver` on a phone-state change, periodically (30-min catch-up for missed broadcasts), and once immediately after the grant (recent history). Gated on `READ_CALL_LOG`.
  - **Messages**: *incoming* texts are captured live by `SmsReceiver` (`RECEIVE_SMS`); *outgoing* texts — which no broadcast can observe — are captured by `SmsBackfillWorker` reading the Sent folder (`READ_SMS`), every 15 min plus once immediately after the grant. Only the peer number + timestamp are ever stored or synced; the message body is never persisted (§6.2 privacy boundary).
- Staged rows land in the encrypted on-device outbox (`pending_interactions`) and are synced by `InteractionSyncWorker` as contact Activities (see the OfflineSyncE2eTest section above).
- Verify on a physical device (Pixel 8a) running a debug build, against the `docker-compose.test.yml` backend:
  ```bash
  docker compose -f docker-compose.test.yml up -d --build
  adb reverse tcp:7300 tcp:7300
  cd android && ./gradlew :app:installDebug
  ```
  Then in the app: log in → Settings → flip "Log calls as activities" on → grant → make/receive a call to a known contact → it appears on that contact's activity feed. Repeat for "Log messages as activities" with a text. Deny the dialog → the toggle snaps back off with a rationale; revoke the permission in system settings and re-enter Settings → the toggle renders off, not "on".
- The unit/Robolectric suite covers the permission state machine (`SettingsViewModelTest`), the grant-gated workers (`CallLogSyncWorkerTest`, `SmsBackfillWorkerTest`), the readers' missing-grant no-op (`CallLogReaderTest`, `SmsHistoryReaderTest`), and the scheduler additions (`TrackingWorkerSchedulerTest`). `TrackingPermissionsE2eTest` exercises the real toggle → flag → catch-up wiring on the harness.
- Distribution note: this app ships **only via F-Droid/Obtainium** — the Play Store restricted-permissions policy for `READ_SMS`/`READ_CALL_LOG` is deliberately out of scope; a future Play build would be a separate feature-stripped flavor and must not compromise this build.

**Android Room migration tests (issue #480)**
- `android/core/data`'s `AppDatabase` (`version = CURRENT_VERSION`, `Migrations.kt`) now exports its schema JSON to `android/core/data/schemas/` on every compile (`exportSchema = true`, wired via `room.schemaLocation` in `core/data/build.gradle.kts`); commit the JSON diff alongside any migration you add. There's no schema JSON below version 16 — `AppDatabase`'s doc comment explains why and what that means for testing.
- `REGISTERED_MIGRATIONS` in `Migrations.kt` is the single source of truth for which version bump has a hand-written `Migration`; `DataModule.provideDatabase` builds its `.addMigrations(...)` call from that list. Run `cd android && ./gradlew :core:data:testDebugUnitTest` to run the whole Robolectric suite, including:
  - `Migration13To14Test` / `Migration14To15Test` / `Migration15To16Test` / `Migration16To17Test` — one class per registered migration, each building a realistic hand-written "before" database (`LocalDatabaseSchemaFixtures.kt`) and asserting it migrates cleanly through the real `AppDatabase` builder. `Migration16To17Test` (issue #479) also asserts every migrated outbox row is backfilled with its per-row `idempotencyKey` (the CON-04/ADR-0010 retry key that makes an ambiguous-failure re-sync replay rather than duplicate).
  - `PendingInteractionsSurviveMigrationTest` — the dedicated, thorough check that `pending_interactions` (the not-yet-synced outbox, the whole reason these migrations are hand-written instead of relying on `fallbackToDestructiveMigration`) survives the full v13→current chain in every field combination the entity supports.
  - `MigrationVersionCoverageTest` — the regression guard: fails if any version pair from `EARLIEST_KNOWN_VERSION` to `CURRENT_VERSION` has neither a registered migration nor an explicit entry in `ACCEPTED_DESTRUCTIVE_GAPS`. Hand-verify it by commenting out an entry in `REGISTERED_MIGRATIONS` and confirming this test fails, then restore it.
  - `DestructiveFallbackTest` — proves the destructive fallback fires (and the app recovers cleanly) for a version gap outside that covered range, and does *not* fire across the real registered chain.
- `app/src/androidTest/.../storage/RoomMigrationEncryptedTest` is the real-device counterpart: the JVM suite above runs against the plain framework SQLite factory (SQLCipher's native lib can't load on the JVM — same carve-out as `RoomCacheEncryptionTest`), so this instrumented test drives the same v13→current migration chain against a real SQLCipher-encrypted file. Runs as part of `./gradlew :app:connectedDebugAndroidTest` (see the Android E2E section above for emulator/device setup) — no separate command.

**Android macrobenchmark (issue #263)**
- The `:macrobenchmark` module ([android/macrobenchmark](android/macrobenchmark)) measures app **cold / warm / hot startup** (`StartupBenchmark`, time-to-first-frame) and **dashboard render** (`DashboardRenderBenchmark`, `FrameTimingMetric` while scrolling the feed that one `/dashboard` call populates). It is a trend signal + local dev tool — **CI never gates on the numbers** (emulator timing variance); turning them into budgets is a separate follow-up.
- It runs against the app's `benchmark` build type (release R8/shrinker config, debug-signed, non-debuggable + profileable — see [android/app/build.gradle.kts](android/app/build.gradle.kts)).
- Startup scenarios need only an emulator/device. The dashboard scenario also needs the `docker-compose.test.yml` backend (it registers the same `e2euser` seed account as the E2E suite, then drives the real login UI).
- Run everything:
  ```bash
  docker compose -f docker-compose.test.yml up -d --build
  cd android
  ./gradlew :macrobenchmark:connectedCheck
  ```
  The `suppressErrors` list that lets this run on an emulator is baked into `macrobenchmark/build.gradle.kts` (`defaultConfig.testInstrumentationRunnerArguments`) — no `-P` flag needed. On a physical device, `adb reverse tcp:7300 tcp:7300` first and add `-Pandroid.testInstrumentationRunnerArguments.serverUrl=http://127.0.0.1:7300` (same as the E2E suite). Startup-only: `./gradlew :macrobenchmark:connectedCheck --tests '*StartupBenchmark'`.
- Results (`*-benchmarkData.json` + Perfetto traces) land under `android/macrobenchmark/build/outputs/`.
- CI runs it via the `android-macrobenchmark` job in [.github/workflows/android-tests.yml](.github/workflows/android-tests.yml) — same emulator + backend as `android-e2e`, same triggers (nightly / manual dispatch / path-gated push to `main`, never on a PR), `continue-on-error` so a red run never blocks. The trace/JSON is kept as a run artifact.

**Pen-test / demo environment (issue #849)**
- A one-command, reproducible instance of the shipped image pre-populated with realistic multi-user data (contacts, relationships, notes, activities, mixed sensitivities, soft-deletes, duplicates) — every seeded user a working login, one an admin, credentials written to the data volume. For hands-on penetration testing and for documentation screenshots.
  ```bash
  docker compose -f docker-compose.pentest.yml up -d --build --wait
  docker compose -f docker-compose.pentest.yml exec mycorrhizal cat /app/data/credentials.json
  docker compose -f docker-compose.pentest.yml down -v   # wipe
  ```
- `PENTEST_PROFILE=demo|smoke|typical|large` selects dataset size (default `demo`: one user, 27 contacts, screenshot-ready). Data is generated fresh from the drift-gated canonical manifest, so it stays current with the schema. Full runbook: [docs/development/pentest-environment.md](docs/development/pentest-environment.md).

**DAST (OWASP ZAP, issue #368)**
- Dynamic application security testing: boots the real all-in-one image (`docker-compose.test.yml`), then runs OWASP ZAP against it — an authenticated API scan seeded from [backend/openapi.yaml](backend/openapi.yaml) (via a minted `full`-scope API token handed to ZAP as a bearer header) plus an active scan of the SPA and the CardDAV/CalDAV discovery surface.
- The scan definition lives in [zap/zap-dast.yaml](zap/zap-dast.yaml); the pass/fail policy lives in [backend/cmd/zapgate](backend/cmd/zapgate) plus the ignore-list [zap/dast.ignore](zap/dast.ignore) (same "ignore-list with justification" shape as `android/.mobsf` and `docker/cis-hardening.ignore`).
- A deliberately-vulnerable **canary** server ([backend/cmd/dastcanary](backend/cmd/dastcanary)) runs alongside the app on `:7301`; its planted reflected-XSS must appear in the ZAP report or the gate fails as "blind". It never ships and is never part of the app.
- CI runs it nightly (not per-PR — DAST is slow/flaky) via [.github/workflows/zap-dast.yml](.github/workflows/zap-dast.yml). The verdict is `zapgate`; the full ZAP `report.json` is kept as a 30-day run artifact. Results are not uploaded to code scanning — a DAST finding has no source line to anchor a SARIF result to (issue #615). To run it locally:
  ```bash
  docker compose -f docker-compose.test.yml up -d --build
  # canary
  (cd backend && go build -o /tmp/dastcanary ./cmd/dastcanary && DAST_CANARY_ADDR=:7301 /tmp/dastcanary &)
  # authenticated session -> bearer token (throwaway user; fresh test DB only)
  BASE=http://localhost:7300/api/v1; U="dast_$RANDOM"; P='DastPassword123!'
  curl -s -c /tmp/c.txt -H 'Content-Type: application/json' -d "{\"username\":\"$U\",\"email\":\"$U@example.com\",\"password\":\"$P\"}" $BASE/register >/dev/null
  curl -s -b /tmp/c.txt -c /tmp/c.txt -H 'Content-Type: application/json' -d "{\"identifier\":\"$U\",\"password\":\"$P\"}" $BASE/login >/dev/null
  TOKEN=$(curl -s -b /tmp/c.txt -H 'Content-Type: application/json' -d '{"name":"dast","scope":"full"}' $BASE/api-tokens | python3 -c 'import sys,json; print(json.load(sys.stdin)["token"])')
  # scan (Linux: --network host; on Docker Desktop use host.docker.internal instead)
  docker run --rm --network host \
    -e ZAP_AUTH_HEADER=Authorization -e "ZAP_AUTH_HEADER_VALUE=Bearer $TOKEN" -e ZAP_AUTH_HEADER_SITE=localhost:7300 \
    -v "$PWD/zap:/zap/wrk:rw" -v "$PWD/backend/openapi.yaml:/zap/openapi.yaml:ro" \
    ghcr.io/zaproxy/zaproxy:stable zap.sh -Xmx3g -cmd -autorun /zap/wrk/zap-dast.yaml
  # gate (policy: ignore-list + canary self-test)
  (cd backend && ZAPGATE_REPORT=../zap/report.json ZAPGATE_IGNORE=../zap/dast.ignore go run ./cmd/zapgate)
  docker compose -f docker-compose.test.yml down -v
  ```
- Notes:
  - The scan is scoped to the throwaway test DB and must never point at real data.
  - The SPA serves unauthenticated static HTML; auth is enforced at the API layer, which the openapi-seeded scan exercises. ZAP's `ZAP_AUTH_HEADER_*` env vars are used because the app's login only sets an httpOnly cookie (no token in the body), which ZAP's JSON-auth method can't extract.
  - CardDAV/CalDAV use Basic auth + WebDAV verbs, which ZAP doesn't spider/fuzz well; the plan seeds the discovery surface by hand. Deep WebDAV fuzzing is issue #512.

**CI: path-gated checks (issue #264)**
- Every suite is **path-gated**: a PR runs only the suites relevant to the files it changes. The mapping from paths to checks lives in one place, [.github/filters.yaml](.github/filters.yaml) — change it there, not per-workflow. Each gated workflow has a `Detect Changes` job (`dorny/paths-filter`) whose boolean outputs the suite jobs `if:` on.
  - `backend/**` → Go fmt/vet/test, govulncheck, gosec, CodeQL(go), backend image build.
  - `frontend/**` → vitest, tsc, eslint, Playwright e2e, CodeQL(js/ts), frontend image build.
  - `android/**` → Android unit/lint/assemble, instrumented e2e, CodeQL(java).
  - `backend/openapi.yaml` → backend + frontend + Android (the API contract).
  - `Dockerfile`, `docker-compose*.yml`, `docker/**` (nginx.conf etc.) → the e2e suites, which build and run the deployed artifact.
  - `.github/**` → everything (a workflow change can affect any check; this includes `filters.yaml` itself).
  - `docs/**` → nothing here; the docs deploy job is already path-filtered to `docs/**`.
- **Why a skipped job is safe to require.** A job skipped by its `if:` conditional reports **Success** (GitHub's current behavior) and satisfies a required status check — unlike a whole workflow skipped by `on: paths:`, which leaves its checks pending and blocks merges. So we gate at the job level, never at the workflow `paths:` level, and every suite check below can be marked required without unrelated pre-existing failures blocking a PR.
- **Required checks on `main`:** `Backend (Go)`, `Frontend (Vitest)`, `Run E2E Tests`, `Android (Gradle)`, `Android E2E (emulator)`. `enforce_admins` is on, so the checks apply to admins too. Set from Settings → Branches (or the REST API, e.g. `gh api`).
- **Verifying the mapping** after touching `filters.yaml`: open small PRs that touch only one area (`android/`, `backend/`, `docs/`, `backend/openapi.yaml`) and confirm each runs exactly the intended suites and reports the rest as skipped.

**CI: flake detection and test-health signal (issue #268)**
- All three suites retry a failed test once in CI and surface the flake instead of silently (or loudly) failing the required check:
  - **Go** runs through `gotestsum --rerun-fails=1` ([unit-tests.yml](.github/workflows/unit-tests.yml)); tests that failed then passed on retry are written to `rerun-fails.txt` (uploaded as an artifact) and printed as "Flaky tests" in the job log.
  - **vitest** sets `retry: 1` and the `junit` + `github-actions` reporters in CI ([vitest.config.ts](frontend/vitest.config.ts)); anything that only passed on retry lands in the job summary's "Flaky Tests" section.
  - **Android** applies `org.gradle.test-retry` to every module's `Test` tasks via the build-logic convention config ([AndroidConfig.kt](android/build-logic/src/main/kotlin/com/mycorrhizal/crm/buildlogic/AndroidConfig.kt)); the `Detect flaky unit tests` step in [android-tests.yml](.github/workflows/android-tests.yml) greps the test-results XML for retained failures after a green run. The instrumented E2E suite is retried once at the step level (the retry plugin only covers JVM `Test` tasks); a retried attempt's failing XML is preserved under `app/build/flaky-attempt-*/`.
- **PR annotations**: [test-report.yml](.github/workflows/test-report.yml) runs on `workflow_run` after the Unit Tests / Android Tests workflows, downloads each suite's JUnit XML artifact, and creates Check Runs with code annotations via `dorny/test-reporter`. It runs in the default-branch context so fork PRs (read-only token) still get annotated; the suite jobs remain the required checks and these reports are informational. A flaky test shows up as a failure in a report while its required check is green — that green-check-plus-failed-report combination *is* the flake-vs-regression distinction.
- **Keep the Scorecard check green on changed workflow lines**: GitHub's Advanced Security Scorecard flags lines added by a PR with a top-level `checks: write` and with unpinned action versions. New/changed workflow steps therefore pin actions by commit SHA with a `# <tag>` comment (the one exception: `test-report.yml`'s job-scoped `checks: write`, which `dorny/test-reporter` genuinely needs to create Check Runs). Older, untouched workflow lines still use plain `@vN` tags; pin them when you touch them.

**Client-side secret scanning (gitleaks, issue #376)**
- GitHub's own secret scanning only sees a secret after it's pushed. To catch one before it ever leaves your machine, this repo ships an opt-in gitleaks pre-commit hook: [.githooks/pre-commit](.githooks/pre-commit) runs `gitleaks protect --staged` against exactly what's staged, using the rule config in [.gitleaks.toml](.gitleaks.toml) (extends gitleaks' default rule pack; no repo-specific rules yet).
- **One-time setup per clone:**
  ```bash
  git config core.hooksPath .githooks
  ```
  Requires the `gitleaks` binary on `PATH` (`brew install gitleaks`, or a release binary from [github.com/gitleaks/gitleaks](https://github.com/gitleaks/gitleaks#installing)). Once configured, the hook is fail-closed: a missing `gitleaks` binary blocks the commit rather than silently skipping the scan. Bypass a single commit with `git commit --no-verify` (e.g. a deliberate false positive you've already reviewed).
  - If a rule ever false-positives on deliberately-fake test material, add a scoped `[[allowlist]]` entry to `.gitleaks.toml` rather than disabling the rule globally.
- This is local-only and doesn't replace server-side scanning — repo admins should also enable GitHub's **push protection** (Settings → Code security → Secret scanning → Push protection), which is a repo setting, not something this hook can turn on.

**Commit sign-off (DCO, OSPS-LE-01.01)**
- Every commit must carry a `Signed-off-by:` trailer matching its author (`git commit -s`) — the [Developer Certificate of Origin](DCO) 1.1. [.github/workflows/dco.yml](.github/workflows/dco.yml) checks every non-merge commit in a PR and fails on a missing or non-matching sign-off; fix a branch with `git rebase --signoff origin/main` then `git push --force-with-lease`. It is not path-gated (it runs on every PR). Add it to the `main` ruleset's required checks alongside the suites listed above. Contributor-facing copy: [docs/development/contributing.md](docs/development/contributing.md#sign-your-commits-dco); project roles and sensitive-resource access: [GOVERNANCE.md](GOVERNANCE.md).

**Data & Integrations**
- SQLite lives at `SQLITE_DB_PATH` (default mycorrhizal.db); migrations in [backend/database/migrations](backend/database/migrations) are embedded into the binary and auto-run on startup.
- JWT expiry, HTTP timeouts, trusted proxies, and Resend email settings are declared in [backend/config/config.go](backend/config/config.go) and loaded based on environment variables; use Config.Validate to catch misconfigurations.
- File uploads stream through [backend/controllers/photo_controller.go](backend/controllers/photo_controller.go) and land in `static/photos`; served through protected routes to enforce auth.
- API consumers expect consistent field casing (e.g., `Firstname` in responses vs. lower-case in queries); follow existing JSON tags in [backend/models/contact.go](backend/models/contact.go).
- Deletions often clean up dependent entities manually (contacts remove reminders, notes, relationships, and activity links); mirror transaction patterns from [backend/controllers/contact_controller.go](backend/controllers/contact_controller.go).

**Dependencies & Updates**

- **Backend (Go modules)**
	1. `cd backend && go mod tidy && go mod verify` to pull new indirect deps, drop unused modules, and confirm checksums.
	2. Use `go get -u ./...` (or target a module) when you intentionally bump versions; commit both go.mod and go.sum together.
	3. Re-run `go test ./...` (or `make test`) plus `make migrate-status` if schema changes shipped with the upgrade.

- **Frontend (Yarn)**
	1. `cd frontend && yarn install --check-files` to sync lockfiles and ensure native binaries rebuild.
	2. For minor bumps run `yarn upgrade` (or `yarn up <pkg>@latest` for a specific lib); keep `yarn.lock` in the PR.
	3. After upgrades, run `yarn build` for production bundles

**Releases (only relevant for maintainers)**

Cutting a release is **one action**: the *Release* workflow
([.github/workflows/release.yml](.github/workflows/release.yml)) → *Run workflow* → enter the
version (e.g. `v0.6.6`). It registers the release's schema fixture
(`internal/schemafixture.SupportedReleases` + a `cmd/genschema` dump) on `main`, commits it, and
pushes a lightweight tag at that commit. The tag push triggers
[docker-publish.yml](.github/workflows/docker-publish.yml), which builds and signs the three
Docker images and the release APK, mints SBOMs + SLSA provenance, and creates the GitHub Release
with generated notes. See `docs/security/release-verification.md` → "How a release is cut".

- **Version**: `vMAJOR.MINOR.PATCH`, and a tag that does not exist yet. A patch release that ships
  no new migration is fine — it shares its predecessor's schema (the fixture dump is byte-identical
  under a new name); `Version:` in `releases.go` is the *migration* version, not a release counter.
- **After dispatch**: watch `docker-publish.yml`. If it fails *after* the tag was pushed, re-run
  **it** from its own *Run workflow* button with the `tag` input (its recovery path) — do **not**
  re-dispatch *Release*, which refuses an existing tag.
- **Do not `git tag` / `git push` a release tag by hand.** A tag pushed with the default
  `GITHUB_TOKEN` (or a plain local push that never runs CI's App-token path) can leave
  `docker-publish.yml` untriggered.

One-time setup (already done for this repo; re-do only if the App is rotated):

- A GitHub App with **Contents: write**, installed on the repo.
- Repo variable `RELEASE_APP_ID` + repo secret `RELEASE_APP_PRIVATE_KEY` (the App's private-key PEM).
- The App added to `main`'s ruleset **bypass list** (`main-protection`) so `release.yml` can push
  the fixture commit and the tag directly. A tag pushed by the App is a real `push` event, which is
  what lets it trigger `docker-publish.yml` — `GITHUB_TOKEN` pushes cannot.

Downstream:

- Users deploy a new version by setting `IMAGE_TAG=v1.5.3` (or just `:latest`) in their `.env` and
  running `docker compose up -d`.
- Each published image carries an SBOM and SLSA provenance attestation and is keylessly signed with
  `cosign`; the release APK carries GitHub-native SLSA build provenance plus an additive cosign
  co-signature. Full operator-facing verification steps (exact commands, what expires and when):
  `docs/security/release-verification.md`.