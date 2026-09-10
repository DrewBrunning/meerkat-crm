---
title: Dependency Upgrade Policy
nav_order: 18
---

# Dependency upgrade policy

**This is the canonical dependency-update policy (COMPAT-03, issue #474).**
Dependency bumps are the most frequent change class in this repo and the one
most likely to be waved through — this page states what gets applied
automatically, what needs a human, how fast a security fix must land, and
when a major version bump is allowed, so the answer stops being "whatever the
person merging felt like."

It covers five ecosystems, each with a genuinely different risk profile and
update cadence: **Go modules** (`backend/go.mod`), **npm/Yarn**
(`frontend/package.json`, `frontend/yarn.lock`), **Gradle**
(`android/**/build.gradle.kts`), **Docker base images** (the three
`Dockerfile`s), and **GitHub Actions** (`.github/workflows/*.yml`).

## Selecting a new dependency

Before the tiers below (which govern *updating* what is already here), a note
on *adding* something new. A new direct dependency is a deliberate choice, made
in its own reviewed pull request — never bundled into an unrelated change, and
never vendored by copying source in. The bar:

- **Is it needed at all?** A few lines of first-party code beat a dependency
  whose transitive tree, release cadence, and abandonment risk we now own.
  This is why the frontend has no lodash-style utility grab-bag and the
  backend leans on the standard library.
- **Is it maintained?** A recent release history, a real issue tracker, and
  more than one contributor. An unmaintained package is a future security
  exception waiting to happen.
- **Is the license compatible?** MIT/BSD/Apache-2.0/ISC and similar are fine;
  copyleft (GPL/AGPL/LGPL) is rejected. This is enforced, not just intended —
  [`license-compliance.yml`](../.github/workflows/license-compliance.yml)
  gates the Go and npm trees, and
  [`dependency-review.yml`](../.github/workflows/dependency-review.yml) runs a
  vulnerability / license / typosquat check on the added dependency in the PR
  diff.
- **How is it obtained?** Through the ecosystem's own package manager into the
  committed lockfile (`go.sum`, `frontend/yarn.lock`, the Gradle version
  catalog) — see the pinning rules below. Nothing is fetched at build time
  from anywhere but the registry the lockfile records.

Once merged, a dependency is **tracked** three ways: the lockfile is the
authoritative list of what resolves; [`syft-sbom.yml`](../.github/workflows/syft-sbom.yml)
publishes a signed SBOM per merge to `main` and per release (see
[`docs/security/release-verification.md`](security/release-verification.md));
and bundled non-manifest content (fonts, icons) is listed in
[`THIRD_PARTY_SOURCES.md`](../THIRD_PARTY_SOURCES.md). Updates from there on
follow the tiers below.

## The three tiers

Every dependency change falls into exactly one of these. The tier is decided
by severity and version-bump size, not by which ecosystem the dependency
belongs to.

### Security fixes

The fastest lane. Triggered by any of: a GitHub Dependabot security alert, a
[grype](../.github/workflows/grype.yml) CRITICAL/HIGH finding (runs on every
push to `main` and nightly, `fail-build: true`), a Trivy CRITICAL/HIGH
release-gate finding (`docker-publish.yml`, fixable CVEs only), a
`govulncheck` reachable-vulnerability finding, or a manually reported
GHSA/CVE against a dependency this project ships.

Two cases, because some of these triggers already gate CI and some only
reach a human as an alert:

1. **Already blocking a live gate** — `govulncheck` (every backend PR and
   nightly, `unit-tests.yml`) and grype (every push to `main` and nightly)
   both hard-fail the build the moment a reachable CRITICAL/HIGH shows up.
   Nothing merges to `main`, and no image gets published (Trivy's release
   gate), while the vulnerable state exists — the "response time" here is
   immediate by construction, not a target to hit.
2. **Reaches only as an alert or report** — a Dependabot Security Advisory
   with no version-update PR yet, a `.grype.yml`-eligible finding that
   `ignore-unfixed` let through the push gate because there is genuinely no
   fix upstream, or a GHSA/CVE reported by hand. Target: **Critical/High
   triaged and either fixed or recorded as a time-bounded exception (below)
   within 5 business days** of the alert appearing — the same acknowledgment
   window [`SECURITY.md`](../SECURITY.md) already commits to for externally
   reported vulnerabilities, reused here for one project-wide number instead
   of a second invented one. **Medium/Low folds into the next weekly
   patch/minor batch** rather than getting its own timeline.

### Patch and minor

Batched and largely automated, because CI is trusted to catch breakage at
this size. `.github/dependabot.yml` already does the batching: weekly PRs per
ecosystem, grouped `minor-and-patch` updates into one PR per group, with a
7-day cooldown (a fresh release gets a week to be pulled if it turns out to
be a compromise before Dependabot offers it). A patch/minor PR merges once
the full relevant CI suite is green — build/test/lint, `dependency-review.yml`
(new-dependency vuln/license/typosquat check on the diff),
`license-compliance.yml` where a manifest changed, and the security gates
above — no separate design review is required beyond skimming the diff and
changelog for anything touching a security-sensitive path (auth, crypto, the
SSRF guards, a hostile-input parser). There is deliberately no auto-merge
workflow: a human still clicks merge, but does not need to re-derive what CI
already verified.

### Major

Never grouped, never automated — `dependabot.yml`'s `minor-and-patch` groups
exclude major bumps by construction, so they always arrive as individual PRs.
Each one is reviewed deliberately: the PR description states what changed
(linking the dependency's own release notes) and why the bump is safe, and —
per "Tying this to the compatibility matrix" below — whether it raises this
project's own supported-runtime minimum. There is no fixed SLA; review
bandwidth decides the pace, but a stale major-bump PR gets closed and
re-opened by Dependabot rather than left to rot indefinitely.

## Pinning rules

Three pinning rules exist today, and all three are enforced mechanically, not
just by habit:

- **The Go toolchain is pinned deliberately** (`backend/go.mod`'s `go` and
  `toolchain` directives) per the security posture in `CLAUDE.md` — it is not
  floated, and a bump is a deliberate edit to that one file, reviewable in
  the normal diff.
- **GitHub Actions are pinned by commit SHA**, not a floating tag — every
  `uses:` line across `.github/workflows/*.yml` names a 40-character SHA with
  the human-readable version as a trailing comment (`actions/checkout@3d3c42e…
  # v7.0.1`). This is not just convention: `zizmor` (`.github/workflows/zizmor.yml`)
  audits every `.github/**` change (plus a weekly re-scan) for, among other
  things, an unpinned or impostor action reference, and hard-fails the check.
  A PR that adds a floating-tag `uses:` line does not merge.
- **Docker base images are pinned by digest**, not just a version tag — every
  `FROM` line in the three Dockerfiles names a `@sha256:…` digest alongside
  the tag (e.g. `golang:1.27.1-alpine@sha256:…`). `hadolint`
  (`.github/workflows/sast.yml`) lints all three Dockerfiles on every
  relevant change and flags an unpinned `FROM`.

**Known gap:** Scorecard's Pinned-Dependencies check still flags the
`npm install` fallback branch in the root and frontend Dockerfiles' dependency-install
conditional (`if [ -f yarn.lock ]; then yarn install --frozen-lockfile; elif …;
else npm install; fi`) as unpinned, because its detector only credits the
literal `npm ci` token. The branch is dead code in this repo today — both
Dockerfiles always `COPY` a committed `yarn.lock`, so the `else` never
executes — but it is a real, open Scorecard finding, not a false alarm this
policy waves away. Tracked, with the reasoning for not removing it yet
(behavior for anyone building from a stripped-down context), at **issue #331**.
This is the one place today where the stated pinning rule and what CI
enforces genuinely diverge; recorded here with its owner rather than
pretending the pinning is complete.

**Permanent exception — the SLSA generator reusable workflow.** The
`apk-provenance` job in `docker-publish.yml` calls
`slsa-framework/slsa-github-generator/.github/workflows/generator_generic_slsa3.yml@v2.1.0`
by **semver tag**, and Scorecard's Pinned-Dependencies check flags it as
"third-party GitHubAction not pinned by hash" (dropping that sub-score to 9).
This one is **by design and will not be fixed**: the SLSA trusted-builder model
requires the reusable workflow to resolve its own ref to establish the builder
identity in the provenance, so `slsa-github-generator` explicitly refuses to
run when referenced by commit SHA — a hash pin would break the very artifact
(`mycorrhizal-apk.intoto.jsonl`) the job exists to produce. Scorecard has no
suppression mechanism for Pinned-Dependencies, so the finding stays visible;
it is accepted here. Upgrades to the generator are a deliberate tag bump
(Dependabot's `github-actions` ecosystem proposes them), reviewed like any
other dependency major. `zizmor`'s equivalent `unpinned-uses` finding is
suppressed inline with `# zizmor: ignore[unpinned-uses]` on that line, and the
verification consequence is documented in
`docs/security/release-verification.md`.

**Gradle is not locked yet, and that is a recorded decision, not an
oversight.** `license-compliance.yml`'s Trivy license scan can only enumerate
Gradle dependency licenses from a `gradle.lockfile`, which this repo does not
emit — so Android's dependency tree is the one ecosystem with no automated
license or (beyond Dependabot's own alerts) vulnerability visibility.
Adopting Gradle dependency locking (`./gradlew dependencies --write-locks`
plus a verify-lockfile CI step) is a real, separate lift — it touches the
Android build's version-catalog wiring, not just a scanner config — and is
deliberately deferred rather than bundled into this policy page as a side
effect. Until it lands, Android dependency risk is covered by Dependabot's
`gradle` ecosystem entry alone (weekly, grouped minor/patch, 7-day cooldown,
same as every other ecosystem) — narrower than the other four, and that
narrowness is the gap this paragraph exists to keep visible rather than
silent.

## When an update cannot be applied

Sometimes the target of the "Security fixes" tier's 5-business-day window
cannot be hit — there is no upstream release yet, the fix is blocked by
another pin (the `nanoid` case in `CLAUDE.md` is exactly this shape: pinned
at the last patched, CommonJS-capable release because nothing newer works
with this build's postcss/vite), or a license question is still open. The
answer is a **recorded, time-bounded exception**, never silence:

[`docs/security/dependency-exceptions.ignore`](security/dependency-exceptions.ignore)
records one entry per unresolved advisory — the advisory ID, ecosystem,
package, when it was opened, when it expires (at most 90 days out — the same
default review period [MAINT-01 (issue #490)](breaking-change-policy.md)
uses for a deprecation window, reused here rather than inventing a second
number), the owner, and why it cannot be fixed yet. `cd backend && go run
./cmd/depexceptions` parses and validates that ledger — malformed entries
fail, and **an entry whose `expires` date has passed fails the check** — and
runs alongside `citecheck` in `unit-tests.yml`'s `backend-checks` job, so it
executes on every backend PR and on the nightly full-suite run
(`unit-tests.yml`'s `schedule` trigger), which means an expired exception
gets surfaced within a day even with no PR activity to trip over it.

This is deliberately a different shape from this repo's existing permanent
ignore lists (`.trivyignore`, `.grype.yml`, `zap/dast.ignore`,
`schemathesis/schemathesis.ignore`, `docker/cis-hardening.ignore`,
`android/.mobsf`, `docs/security/citation-drift.ignore`,
`docs/security/crypto-surface.ignore`). Those record "the scanner is right
that this pattern exists, but it does not apply to how we use this
dependency" — a decision that can stand forever once written down. The
dependency-exceptions ledger records the opposite situation: a real,
applicable advisory that is temporarily unfixed. An exception that is still
open when it expires must be resolved (apply the fix) or renewed (a fresh
dated entry with an updated reason) — it cannot just sit there.

## Tying this to the compatibility matrix

A dependency bump that raises this project's own supported-runtime minimum —
[`docs/development/supported-runtime-matrix.md`](development/supported-runtime-matrix.md),
**issue #472** — is a breaking change under
[`breaking-change-policy.md`](breaking-change-policy.md) (**MAINT-02, issue #491**),
which lists "raises a supported-version minimum" as one of its
breaking-change categories. It is not an incidental side effect of a routine
bump. Concretely: before merging a bump that changes an `engines` field, a
`go`/`toolchain` directive, `minSdk`, or a base-image line whose own new
version declares a higher runtime requirement than the matrix currently
states, check whether it actually raises that floor. If it does, the PR
needs the breaking-change process that page describes (a deprecation window
per MAINT-01 if anything currently relies on the lower floor, a release note,
and — post-`1.0.0` — a major version bump), not a same-day merge on green CI.
This applies regardless of tier: even a nominally "patch" release of a
dependency can raise its own declared minimum Node/Go/JDK version.

MAINT-03 (issue #492), when it lands, classifies dependency-update issues
consistently with the tiers this page defines — this page is the source of
truth those classifications point back to, not a second one.
