---
title: Release gates
parent: Development
nav_order: 8
---

# Release gates

**This is the canonical mandatory-gate statement (REL-03, issue #447).** It defines which checks
must pass before a `v*` tag is allowed to publish, at which **tier**, with what explicit
**pass/fail criterion**, and how a failure actually blocks publication rather than being noticed
afterward.

The machine-readable twin is [`.github/release-gates.json`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/release-gates.json).
`cd backend && go run ./cmd/releasegatecheck` (a `Docs & security-doc citations` step, every PR)
fails the build if the JSON is malformed, if any gate names a workflow that does not exist, or if
this table and the JSON disagree. The `release-gate` job in `docker-publish.yml` reads the same
JSON.

This page is contributor- and maintainer-facing; it assumes a repo checkout and the Go toolchain
for the `go run` command above.

## Tiers

| Tier | Meaning |
|---|---|
| **per-pr** | Fast enough for every pull request. Blocks merge via the `main-protection` ruleset, and is **re-checked on the release commit** by the `release-gate` job (for the subset marked `release_gate` in the JSON). |
| **release-internal** | A job inside `docker-publish.yml`. Enforced by that workflow's `needs:` graph — if it fails, no release, no images, no APK. |
| **release-tier** | Too slow for every PR (nightly / `push: main` / on-dispatch). The [REL-06 release workflow (#499)](https://github.com/DrewBrunning/mycorrhizal-crm/issues/499), `release.yml`, triggers the ones with no `push: main` trigger (`min-version-tests`, `zap-dast`) and waits on every release-tier run for the fixture commit before it pushes the tag — an observed failure means the tag is never pushed, a 75-minute deadline with a run still going is a `::warning::` and the tag proceeds. |
| **advisory** | Runs and is visible, but a failure does not block a release. A regression is triaged, not gating. |

## How publication is blocked

Four mechanisms, in order of when they fire:

1. **Merge time** — the `main-protection` branch ruleset requires the per-PR check contexts, so
   a failing gate cannot reach `main` in the first place (the merge-time counterpart is
   [#508](https://github.com/DrewBrunning/mycorrhizal-crm/issues/508)).
2. **Cut time** — the [REL-06 workflow (#499)](https://github.com/DrewBrunning/mycorrhizal-crm/issues/499),
   `release.yml`, is the single human action that cuts a release. Before it commits the schema
   fixture or pushes anything it runs the mandatory gate battery: `go run ./cmd/citecheck` and
   `go run ./cmd/releasegatecheck` directly; a deterministic poll of every `release_gate: true`
   context on the commit `main` is at; and the ASVS/MASVS re-verification obligation
   ([#608](https://github.com/DrewBrunning/mycorrhizal-crm/issues/608)) — the report's §10
   changelog must carry a new row since the previous release tag, unless the dispatch supplied
   `ack_asvs_current` with a reason. After the fixture commit it triggers and waits on the
   release-tier suites (above). Any failure means no tag is pushed, so `docker-publish.yml`
   never starts. `dry_run: true` runs this whole battery and stops before any write.

   **Dispatch, not just poll, for the mandatory per-PR gates**
   ([#543](https://github.com/DrewBrunning/mycorrhizal-crm/issues/543)): a real RC cut found
   every one of the 9 `release_gate: true` per-PR checks sitting at `missing` — not slow,
   structurally absent — for the entire deadline. Root cause: `unit-tests.yml`, `e2e-tests.yml`,
   `android-tests.yml`, `sast.yml`, `zizmor.yml`, and `container-hardening.yml` trigger on
   `push: branches: [main]` only, never `release/**` (deliberately — a `push: release/**`
   trigger would give cache-write access to anyone who can push the branch, exactly what
   zizmor's cache-poisoning audit exists to catch). A final release (cut from `main`) already
   gets a real `push:main`-triggered run of each on the exact release commit; an RC (cut from
   `release/vX.Y.0`) never does. So before polling, this step now dispatches each mandatory
   gate's workflow via `workflow_dispatch` against the release ref — skipping the dispatch only
   when `push:main` already covers it — the same shape as the release-tier suites just above.
   Each of those six workflows' path-gating (`changes` job) forces every area `true` on a
   `workflow_dispatch` event, so the dispatched run actually exercises everything rather than
   skipping for lack of a diff to gate on. The poll deadline moved from 30 to 75 minutes to
   match: dispatched runs need real time now that they're genuinely executing (`Android
   (Gradle)` alone budgets 40 minutes). Each dispatch's timestamp is recorded and, when reading
   back check-run state, a gate ignores any check-run that started before its own most recent
   dispatch this run (issue #1013) — re-dispatching the same workflow+ref more than once against
   one commit (a retry while debugging, a re-run) leaves multiple check-runs sharing a name on
   that commit, and an older one can read back `cancelled` (superseded by the workflow's own
   `concurrency:` group) while the fresh dispatch is still in flight; without the cutoff that
   stale conclusion looks like this run's result and hard-fails the gate before the new dispatch
   ever gets a chance to complete.
3. **Publication time** — `docker-publish.yml`'s **`release-gate`** job is the first thing that
   runs on a tag push. Same dispatch-before-poll shape as cut time: a final release's mandatory
   gates already ran via `push:main`, so this job dispatches them only for an RC tag, then polls
   the release commit's check-runs and commit statuses for every gate marked `release_gate: true`.
   The semantics are deliberately asymmetric:
   - an **observed** `failure` / `cancelled` / `timed_out` / `action_required` on a mandatory
     gate is a **hard block** — `create-release`, `build-and-push`, `build-android-apk`, and
     `schema-fixture-gate` all `needs:` this job, so nothing publishes;
   - **all gates green** (`success` / `neutral` / `skipped` — a path-skipped suite reports
     success by design, #264) → pass;
   - the **75-minute deadline** (was 60; see the #543 note above) with a gate still not
     reporting a conclusion → a loud `::warning::` and publication **proceeds**. GitHub check
     timing (a slow fuzz leg, an aggregation job that has not run yet) is not a quality signal.
4. **The `needs:` graph** — the `release-internal` gates enforce themselves: `build-and-push`
   `needs: build-android-apk`, `create-release` `needs: build-android-apk`, everything
   `needs: release-gate`. `verify-release-assets` is the final belt-and-suspenders check that
   the APK and its cosign bundle actually landed on the Release and every image tag resolves.

## Override policy

The **only** way past a failed `release-gate` is to run `docker-publish.yml` from the Actions
tab (**Run workflow**) with a non-empty **`override_reason`** input. The job then skips the poll
and writes `RELEASE GATE OVERRIDDEN by <actor>: <reason>` to the run summary — explicit, logged,
and attributed. A tag **push** has no override path at all. Nothing is ever a silently skipped
job.

## The Android decision (issue #527)

**A release hard-blocks on a green, keystore-signed, `apksigner`-verified APK that lands on the
GitHub Release.** `create-release` and `build-and-push` both `needs: build-android-apk`, so a
signing or build failure blocks the entire release — Docker images included. This deliberately
reverses the earlier "a signing problem here shouldn't block the Docker images" decoupling:
`v0.7.0`'s premise is that a release is one artifact set, and the Android client is part of it.

`build-android-apk` now, after `assembleRelease`:

- runs **`apksigner verify --print-certs`** and fails if the APK is not signed (v2/v3 scheme).
  When the repo variable **`ANDROID_SIGNING_CERT_SHA256`** is set, it also asserts the signer
  certificate SHA-256 matches; otherwise it warns that the cert is not pinned. Set that variable
  to the release keystore's cert fingerprint to enforce.
- asserts the built APK's **`versionCode`** equals the value the workflow computed
  (`1000 + GITHUB_RUN_NUMBER`, strictly increasing per [the versioning policy](../versioning-policy.md))
  and is `> 1` — a light pin on the plugin's override wiring so an in-place upgrade keeps
  working. The full "install release N, then N+1" emulator test is
  [#480](https://github.com/DrewBrunning/mycorrhizal-crm/issues/480).
- asserts `app-release.apk` and `mycorrhizal-apk.sigstore.json` are present on the Release.

**PKCS12 keystore note:** `SIGNING_KEY_PASSWORD` **must equal** `SIGNING_STORE_PASSWORD` for this
keystore. A mismatch fails `assembleRelease` with an opaque padding error, not a clear message.

## The gates

Generated view of `.github/release-gates.json`. `releasegatecheck` asserts every row here has a
matching registry entry (name, tier, mandatory) and every `workflow` file exists.

<!-- release-gates:begin -->

| Gate | Tier | Mandatory | Pass criterion | Workflow |
|---|---|---|---|---|
| `Backend (Go)` | per-pr | yes | go build + go vet + gofmt clean and `go test ./... -race` passes; no package exceeds its -timeout. | `unit-tests.yml` |
| `Frontend (Vitest)` | per-pr | yes | `tsc --noEmit` and `vitest run` both pass. | `unit-tests.yml` |
| `Run E2E Tests` | per-pr | yes | the route-stubbed Playwright suite (including @perf specs) passes. | `e2e-tests.yml` |
| `Android (Gradle)` | per-pr | yes | `testDebugUnitTest`, `lintDebug`, `detekt`, and `assembleDebug` all pass. | `android-tests.yml` |
| `Android E2E (emulator)` | per-pr | yes | the instrumented suite passes against the docker-compose.test.yml backend on an API-26 emulator. | `android-tests.yml` |
| `Android scan (mobsfscan)` | per-pr | yes | mobsfscan reports no new high-severity finding on the Android sources. | `sast.yml` |
| `Scan workflows (zizmor)` | per-pr | yes | zizmor exits 0 — no finding above what zizmor.yml's ignore list accepts. | `zizmor.yml` |
| `CIS container hardening scan` | per-pr | yes | the all-in-one image passes docker/cis-hardening.sh and the Trivy misconfig/secret scan with no CRITICAL/HIGH. | `container-hardening.yml` |
| `codecov/patch/backend` | per-pr | yes | changed Go lines are >= 95% covered (codecov.yml). Merge-time only: a PR-diff concept, not re-polled at publication. | `unit-tests.yml` |
| `codecov/patch/frontend` | per-pr | yes | changed TypeScript lines are >= 90% covered. Merge-time only. | `unit-tests.yml` |
| `codecov/patch/android` | per-pr | yes | changed Kotlin lines are >= 80% covered. Merge-time only. | `android-tests.yml` |
| `Detect Changes` | per-pr | yes | the shared path-filter job completes; always green (structural). Required so path-skipped suites can be required checks (#264). | `unit-tests.yml` |
| `Docs & security-doc citations` | per-pr | yes | citecheck + depexceptions + deprecations + docscheck + releasegatecheck all exit 0. Runs on every PR and nightly. `release_gate: true` — polled on the release commit, and `release.yml` also runs `citecheck` directly as a hard gate (#608). | `unit-tests.yml` |
| `Migration Tests` | per-pr | yes | every supported-release upgrade leg, adjacent hop, and down round-trip passes. Per-leg check names make polling impractical; the release commit only adds a frozen schema dump, which schema-fixture-gate verifies, and the push:main run covers the chain. | `migration-tests.yml` |
| `Go binary reproducible` | per-pr | yes | two builds from different paths are byte-identical (REL-04). Runs on the release commit's push:main; not in the ruleset. | `reproducibility.yml` |
| `validate-tag` | release-internal | yes | the pushed tag matches the versioning-policy pattern (REL-01, backend/internal/versionpolicy). Blocks every downstream job. | `docker-publish.yml` |
| `release-gate` | release-internal | yes | dispatches each mandatory release_gate:true gate's workflow for an RC tag (#543 — a final release already got them via push:main); no mandatory gate is observed FAILED on the release commit; all-green passes; a 75-minute deadline with a gate still not reporting is a warning and publication proceeds. A workflow_dispatch run with a non-empty override_reason skips the poll and records the override with the actor. | `docker-publish.yml` |
| `schema-fixture-gate` | release-internal | yes | a committed backend/database/testdata/schemas/<tag>.sql exists for a mycorrhizal-supported-series tag (MIG-01, #436/#529). | `docker-publish.yml` |
| `build-and-push` | release-internal | yes | the multi-arch images build and push; each digest gets a cosign keyless signature, an SBOM, and SLSA build provenance. | `docker-publish.yml` |
| `build-android-apk` | release-internal | yes | the release APK assembles, is keystore-signed, `apksigner verify` passes (and matches ANDROID_SIGNING_CERT_SHA256 when set), its versionCode equals the computed value and is > 1, a GH build-provenance attestation + a cosign bundle are produced and attached to the Release, and its sha256 subject is exported for the SLSA generator. | `docker-publish.yml` |
| `apk-provenance` | release-internal | yes | the `slsa-github-generator` reusable workflow signs the APK subject and emits `mycorrhizal-apk.intoto.jsonl` (SLSA build provenance) as a workflow artifact (issue #355). | `docker-publish.yml` |
| `verify-release-assets` | release-internal | yes | attaches `mycorrhizal-apk.intoto.jsonl` and a `SHA256SUMS` manifest to the Release, then asserts the Release carries `app-release.apk`, `mycorrhizal-apk.sigstore.json`, `mycorrhizal-apk.intoto.jsonl` and `SHA256SUMS`, and every published image tag resolves in the registry. | `docker-publish.yml` |
| `Test minimum supported versions` | release-tier | yes | the app builds and the suite passes against each declared minimum runtime (COMPAT-02, #473). | `min-version-tests.yml` |
| `Migration at scale (large dataset)` | release-tier | yes | with MYCORRHIZAL_LARGE_TESTS=1, every supported release migrates to current at ~134x the canonical manifest with row counts and integrity intact (#495). | `migration-tests.yml` |
| `CardDAV real-server E2E` | release-tier | yes | a full round trip against the real reference servers matches the divergence register (#496). | `carddav-e2e.yml` |
| `Constrained-resource chaos` | release-tier | yes | the disk-full / mem-limited / cpu-limited jobs fail closed with no corruption (#498). | `chaos-tests.yml` |
| `Schemathesis API fuzzing` | release-tier | yes | no unhandled 500, no response that violates backend/openapi.yaml, no BOLA finding. | `schemathesis.yml` |
| `Differential E2E (calcard)` | release-tier | yes | our JSContact / iCalendar output matches the pinned reference implementations (#680). | `differential-e2e.yml` |
| `Reference-clients E2E` | release-tier | yes | vdirsyncer round-trips against our server with no data loss (#681). | `reference-clients-e2e.yml` |
| `ZAP DAST` | release-tier | yes | the baseline scan raises no new high-risk dynamic finding. | `zap-dast.yml` |
| `OpenSSF Scorecard` | advisory | no | informational supply-chain posture; a score drop is reviewed, never release-blocking. | `scorecard.yml` |
| `CodeQL` | advisory | no | SARIF is uploaded; a new alert is triaged in the Security tab, not release-blocking. | `codeql.yml` |
| `Grype vulnerability scan` | advisory | no | second-opinion CVE scan; the critical/high hard gate is on main + nightly, advisory at release time. | `grype.yml` |
| `TruffleHog secret scan` | advisory | no | verified-secret git-history scan; a hit is investigated immediately but is not a release job. | `trufflehog.yml` |
| `Stryker mutation testing` | advisory | no | mutation-score trend for the core modules; advisory. | `stryker.yml` |
| `Android macrobenchmark` | advisory | no | cold/warm/hot startup and dashboard frame timing trend; continue-on-error by design (emulator variance). | `android-tests.yml` |
| `Reproducible image (all-in-one)` | advisory | no | double-build config + layer digest compare for the linux/amd64 image; continue-on-error until reliably green on main (REL-04, #448). | `reproducibility.yml` |

<!-- release-gates:end -->

## Adding or changing a gate

Edit `.github/release-gates.json` **and** the table above in the same PR (`releasegatecheck`
fails otherwise). A new `release_gate: true` entry must be `per-pr`, have a real `check_run`
context, and be added to the `main-protection` ruleset separately (that config lives in GitHub,
tracked by #508). Moving a suite from `release-tier` to `per-pr` means it is now fast enough to
poll — say so in the PR.
