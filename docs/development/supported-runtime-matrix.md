# Supported runtime matrix (issue #472)

This is the engineering source of truth for every supported-version claim this project makes.
A minimum with no reason gets raised or lowered arbitrarily the next time someone finds it
inconvenient; every row below states the minimum **and** why it is there, grounded in an
actual dependency, feature, or file in this repo rather than a round number.

COMPAT-01 (issue #472) produced this matrix and wired the two rows that can be mechanically
enforced (`browserslist`, `engines`) into the real build/CI. COMPAT-02 (issue #473) adds the CI
jobs that build/test at every row's stated minimum — see "Minimum-version CI coverage" below —
plus below-floor jobs proving an unsupported version fails clearly rather than something
silently broken. The operator-facing half is published as
[../supported-versions.md](../supported-versions.md) (DOC-01, issue #486): what this runs on,
what "supported" means (tested in CI vs expected-to-work vs unsupported), what happens on an
unsupported version, and the supported deployment shape — it summarizes this table for
`docs/getting-started.md` / `docs/deployment.md`'s audience rather than duplicating it, and a
structural check (`cmd/docscheck`, DOC-04 issue #489) fails until the two stay in agreement. The
dependency-*upgrade policy* (how/when a floor is allowed to move) is
COMPAT-03 (issue #474) and [breaking-change-policy.md](../breaking-change-policy.md)
(MAINT-02, issue #491) — raising any row below is a breaking change under that policy, not a
routine edit. This table states what the *server* requires to run; the
[client/server compatibility policy](../client-compatibility-policy.md) (ANDROID-01, issue
#478) is the client-facing counterpart — what server versions require of the Android app and
web client talking to them. It covers supported *versions*; the supported **deployment
shape** — multi-user-per-instance, the cross-user isolation guarantee, and what an admin
can see — is [`supported-configuration.md`](../supported-configuration.md) (issue #558).

| Component | Minimum | Why |
|---|---|---|
| **Go** | `1.26.0` (toolchain `1.27.1`, `backend/go.mod`) | Deliberately pinned per the security posture (see CLAUDE.md) — this row states the current pin, it does not float it. Contributors building from source need a matching Go install; the shipped Docker image does not (it's built inside a `golang:1.27.1-alpine` build stage). |
| **Node.js** (contributor/CI, not the shipped image) | `>=22.22.2` (`frontend/package.json` `engines.node`) | The binding constraint is a transitive dependency's own `engines` field, not a floor this project chose: `jsdom@30` (a `devDependency`, vitest's DOM environment) declares `engines.node: "^22.22.2 \|\| ^24.15.0 \|\| >=26.0.0"`. On the 22.x line that pins the floor to the patch, not the minor — `22.22.0`/`22.22.1` fail `yarn install` outright with `.yarnrc`'s `engine-strict`. `react-router@8.x` (a runtime dependency) separately declares `engines.node: ">=22.22.0"`, looser than jsdom's but still well above vite@8.2.2/eslint@10's own `^20.19.0 \|\| >=22.13.0`. Found by COMPAT-02 (issue #473) hand-verifying this row: `yarn install --frozen-lockfile` at the previously-declared `22.13.0` and `22.22.0` both fail today — the floor had already silently drifted upward via a dependency bump before this ticket, which is exactly the failure mode #473 exists to catch. Separately, `frontend/vitest.config.ts` unconditionally passes `--no-experimental-webstorage` to the test worker, a flag that does not exist before Node 22.4 — moot now that jsdom's floor is tighter, but it's *why* the 20.x branch was dropped in the first place (COMPAT-01). The all-in-one Docker image builds the frontend itself inside a pinned `node:26-alpine` stage (`Dockerfile`), so an operator running the published image never needs a local Node at all — this row is for anyone building from source. |
| **Yarn** | Classic v1, `>=1.22.0` (`frontend/package.json` `engines.yarn`) | Matches the committed `yarn.lock` v1 format and the version already used in CI/dev; nothing in this repo needs a newer Yarn Classic release, and migrating to Yarn Berry is a separate, undecided change (see the `nanoid`/postcss CommonJS constraint in CLAUDE.md, which is unrelated but shows the toolchain is deliberately conservative). `frontend/.yarnrc`'s `engine-strict true` makes this enforced, not decorative — Yarn Classic does not check `engines` by default. |
| **Browsers** | Chrome/Edge/Firefox ≥ 111, Safari/iOS ≥ 16.4 (`frontend/package.json` `browserslist`) | The binding constraint is Web Push: `frontend/src/pushSubscription.ts` uses `PushManager`/`applicationServerKey`, and Safari only shipped Web Push support in **16.4** (March 2023). Every other evergreen engine has supported Web Push and service workers for far longer, so the other three floors are pinned to the same release window (~March 2023) for one coherent, testable statement rather than a false sense of a lower floor nothing else in the PWA feature set was ever exercised against. `frontend/vite.config.ts`'s `build.target: browserslistToEsbuild()` reads this array directly, so it constrains the actual build output — not just documentation. |
| **SQLite** | Ships via the pure-Go `glebarez/sqlite` driver | No separate host SQLite install; the version travels with the Go module, governed by the Go row above. **Storage constraint, not a version number: local filesystem only.** SQLite's WAL mode depends on advisory byte-range locks that NFS, SMB/CIFS, and similar network filesystems do not reliably implement across clients — running the database over one is a documented corruption risk, and it is the most likely self-hosted mistake (mounting a NAS share and pointing `SQLITE_DB_PATH` at it). `backend/internal/fsguard` warns loudly at startup when it detects a known network-filesystem type under the database path (see "Fail-clearly behavior" below) but cannot catch every case — see its own doc comment for the FUSE caveat. |
| **Docker Engine** | `>=23.0` | Every doc and script in this repo invokes `docker compose` (the Compose **V2** CLI plugin) — never the deprecated hyphenated `docker-compose` v1 binary. Compose V2 became the Engine-bundled default at 23.0. Nothing in the committed `docker-compose*.yml` files uses syntax newer than that (checked this session: no `develop:`, `include:`, or other recent top-level keys — just `services`, `healthcheck`, `environment`, `volumes`). |
| **Docker Compose** | V2 (any release bundled with Engine `>=23.0`) | Same reasoning as above; there is no independent Compose-only floor beyond "whatever ships with the Engine minimum." |
| **Host OS / architecture** | Linux, x86_64 or arm64, running Docker | The only deployment shape this project builds, tests, and documents (`docker-publish.yml` publishes multi-arch images). Anything else — bare-metal install, Windows/macOS host, a non-Docker container runtime — is unsupported, not merely undocumented. |
| **Android** | `minSdk 26` (Android 8.0 Oreo; `android/build-logic/src/main/kotlin/com/mycorrhizal/crm/buildlogic/AndroidConfig.kt`) | `MycorrhizalApplication.kt`/`MycorrhizalApp.kt` create `NotificationChannel`s, an API-26 concept the app's notification features (push, ntfy, Gotify) depend on — there is no lower-API code path for notifications, so `minSdk` below 26 would need one built first. **What would justify raising it:** a feature that needs a platform API above 26 with no reasonable fallback. Raising the floor drops real devices still on Android 8–9 and is a breaking change under [breaking-change-policy.md](../breaking-change-policy.md) (MAINT-02, issue #491) — it needs that process, not an incidental bump alongside an unrelated change. |

## Fail-clearly behavior (issue #472 action 6 / #540's "unsupported environments fail clearly")

What is checkable at runtime, and what happens when the floor isn't met:

- **Storage filesystem type** — `backend/internal/fsguard.NetworkFilesystemWarning`, called from
  `main.go` right after `database.InitDB` succeeds. Detects NFS/SMB/CIFS/SMB2/NCP/Coda/AFS under
  the configured `SQLITE_DB_PATH` and logs a clear, actionable warning naming the filesystem —
  deliberately **advisory, not fatal** (see the function's doc comment): this is a corruption
  *risk*, not a certainty, and real production data already exists on deployments this check has
  never run against. Refusing to boot could brick a currently-working instance with no escape
  hatch; a v0.6.0-upgrade-floor-style hard refusal was considered and rejected for that reason.
- **Node/Yarn** — `frontend/.yarnrc`'s `engine-strict true` makes `yarn install` refuse outright
  on a Node/Yarn outside `engines`, both locally and in CI (`actions/setup-node`'s
  `node-version-file: frontend/package.json` reads the same `engines.node` value, so CI always
  runs the declared floor rather than an independently hardcoded number).
- **Browsers** — `build.target` (derived from `browserslist`) constrains what syntax the bundle
  can emit, so a browser between ES-module support and the stated floor gets a build-time
  guarantee: it either runs correctly or fails with a real parse/runtime error, never silent wrong
  behavior. Below ES-module support entirely (roughly 2017-18, well under the floor) is the worse
  case COMPAT-02 (issue #473) closed: `index.html`'s `<script type="module">` entry point is
  silently skipped by such a browser with no console error at all, which used to mean a
  permanently blank white page. `<script nomodule src="/unsupported-browser.js">` fixes it — a
  browser without module support runs that file instead, by construction, and it replaces `#root`
  with a plain-language upgrade message. It's a separate file rather than an inline script because
  the CSP above (`script-src 'self'`, no `'unsafe-inline'`) would otherwise silently drop it for
  exactly the CSP-aware browsers just below the floor.
- **Go, Docker/Compose, host OS** — not checkable from inside this codebase (Go is a build-time
  toolchain choice; Docker/Compose/host-OS versions are outside any process this project controls
  at boot). Documented here instead, per the issue's "a startup check for the things that are
  checkable, and a documented statement for the things that are not."

## Minimum-version CI coverage (COMPAT-02, issue #473)

Every row above is exercised at its stated floor by a CI job — not the current/pinned version
every other workflow in this repository builds and tests with. `backend/internal/compatci` is the
structural coupling: a matrix row with no entry in `compatci.MatrixCoverage` (or one naming a
workflow/job that doesn't exist) fails `TestMatrixRowsHaveCoverage`, so this table and the jobs
below cannot drift apart silently.

| Row | Job(s) | Notes |
|---|---|---|
| Go | `min-version-tests.yml` / `go-minimum`, `go-below-minimum` | `GOTOOLCHAIN=local` so go.mod's `toolchain` line can't silently mask the floor. |
| Node.js, Yarn | `min-version-tests.yml` / `node-yarn-minimum`, `node-below-minimum` | Yarn Classic run via a pinned standalone download, not whatever the runner ships. |
| Browsers | `min-version-tests.yml` / `browser-minimum` | Real, pinned Firefox 111 (exact floor) and Chrome for Testing 115 (closest official artifact to the 111 floor — Google publishes no pinned, downloadable Chrome 111) driven via raw WebDriver. Below-floor case is `frontend/src/unsupportedBrowserFallback.test.ts` (a unit test, not a live ancient browser — module/nomodule dispatch is a guaranteed HTML5 behavior, not project code). |
| SQLite | rides on Go | No version of its own; travels with the Go module. |
| Docker Engine, Docker Compose | `min-version-tests.yml` / `docker-compose-minimum` | Validates the compose-file-syntax floor with the earliest V2 client against the runner's current dockerd — does **not** run an actual old Engine daemon (see the job's own comment for why that's a deliberately separate, higher-risk piece of work). |
| Host OS / architecture | not independently checkable | Linux x86_64/arm64 is the only shape this project builds/tests at all. |
| Android | `android-tests.yml` / `android-e2e-min-sdk`, `android-below-min-sdk` | Real instrumented E2E suite at API 26; below-floor asserts `adb install` fails with `INSTALL_FAILED_OLDER_SDK` on API 24. |

All of the above are scheduled weekly (`min-version-tests.yml`) or nightly (the Android jobs, on
`android-tests.yml`'s existing cadence) plus `workflow_dispatch` — deliberately not on every PR,
since the floor moves rarely and several of these jobs download and boot a second browser/Go
toolchain/Compose client. `min-version-tests.yml` also exposes `workflow_call` for a future
required pre-release gate (REL-03, issue #447) to invoke without editing the workflow.
