---
title: Versioning policy
nav_order: 12
---

# Versioning policy

**This is the canonical versioning statement (REL-01, issue #445).** It defines
what a version number means for this project, what a release tag may look like,
where the version comes from, and how release candidates are identified. The
tag-format gate in `.github/workflows/docker-publish.yml`, the validation in
`.github/workflows/release.yml`, and the `versionpolicy` package
(`backend/internal/versionpolicy`) are all defined against this document; a
change here is a change to those, and vice versa (see
[Document consistency](#document-consistency)).

This page is contributor- and maintainer-facing. Operators do not choose
version numbers; what an operator needs — whether an upgrade requires anything
of them — is the [changelog](https://github.com/DrewBrunning/mycorrhizal-crm/releases)
and the [upgrade-compatibility policy](upgrade-compatibility.md).

## Semantic versioning

Mycorrhizal CRM versions are [SemVer 2.0.0](https://semver.org): `MAJOR.MINOR.PATCH`,
tagged with a leading `v`. What the three components mean **here**:

| Component | Incremented when | Example |
|---|---|---|
| **MAJOR** | A breaking change to a covered surface ships. Post-`1.0.0` this is the deprecation-and-removal endpoint, not a routine event. | `1.0.0` → `2.0.0` |
| **MINOR** | Backward-compatible functionality is added — a new endpoint, field, config variable, export element, CLI flag. | `1.2.0` → `1.3.0` |
| **PATCH** | Backward-compatible bug fixes only; no new surface. | `1.2.3` → `1.2.4` |

**What counts as breaking is not decided here.** It is decided once, in
writing, by the [breaking-change policy](breaking-change-policy.md) (MAINT-02,
issue #491) — covered surfaces, the breaking/additive line, and the fact that a
breaking change is `2.0.0`, never a parallel `/api/v2`. The *removal window* for
anything that policy classifies as breaking is the
[deprecation policy](deprecation-policy.md) (MAINT-01, issue #490). This page
points at both rather than restating them.

## The pre-1.0 rule, as it actually is

**`0.x` makes no backward-compatibility promise.** Breaking API and contract
changes still happen in `0.x`; this is pre-1.0 software and CLAUDE.md says so
plainly. The roughly-one-release-a-day cadence between `v0.2.0` (2026-08-04) and
`v0.6.0` (2026-08-22) was legitimate under this rule, not sloppy.

Two things are *not* waived pre-1.0:

- **Data preservation.** Since `v0.2.0-alpha-candidate` there is real production
  data. A migration that loses data is breaking regardless of version
  (CLAUDE.md: "breaking *data* is a different, higher bar"). This is independent
  of the SemVer component being bumped.
- **The supported-upgrade floor.** In-place upgrade is supported from `v0.6.0`
  and later, version-skipping included — see
  [upgrade compatibility](upgrade-compatibility.md) (issue #529).

The `/api/v1` compatibility promise — no removals, renames, or narrowing within
a major line — begins at `1.0.0`. See the breaking-change policy for its exact
wording.

## Release-tag format

A release tag is:

```
^v[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$
```

That is: a leading `v`, three dot-separated non-negative integers, and an
**optional** `-rc.N` pre-release suffix (`N` a positive integer). Nothing else —
no `-alpha`, no `-beta`, no `+build` metadata, no date suffix, no `.0` fourth
component.

- The exact expression above is the one authoritative copy. It is embedded
  verbatim in `backend/internal/versionpolicy/tagpattern.go` (`TagPattern`), in
  the `release.yml` version check, and in the `docker-publish.yml` tag gate; a
  test fails if the three drift apart.
- **A tag that does not match fails CI.** `docker-publish.yml` publishes on
  `push: tags: ['v*']`; its first job (`validate-tag`) rejects any pushed tag
  that violates the pattern, and the release/image/APK jobs depend on it, so a
  malformed tag produces no GitHub Release, no images, and no APK.
- The historical `v0.2.0-alpha-candidate` tag predates this policy. It is never
  re-pushed, so it needs no exception in the gate; it simply would not be
  accepted today.

### Release candidates

`0.9.x` is a release-candidate series by design (the RC milestone). **RC tags
are `vMAJOR.MINOR.PATCH-rc.N`**, numbered from `1`:

```
v1.0.0-rc.1
v1.0.0-rc.2
...
```

The RC series leading to `1.0.0` is tagged `v1.0.0-rc.N`. The `v0.9.x` milestone
is the planning bucket for that work; the tags it produces are the
`v1.0.0-rc.N` series, not a `v0.9.x` release line.

Why `v1.0.0-rc.N` and not `v0.9.0`, `v0.9.1`, …:

- It is standard SemVer. A hyphenated identifier **is** a pre-release by
  definition, so "mark the GitHub Release as a pre-release" and "keep it off the
  Obtainium auto-update channel" become mechanical rather than a naming
  convention someone has to remember (RC-02, issue #446).
- It sorts correctly: `v1.0.0-rc.1 < v1.0.0-rc.2 < v1.0.0`. A `v0.9.x` scheme
  sorts the RCs as ordinary releases *below* `v1.0.0` with no pre-release
  signal.

RC **publication mechanics** — the pre-release flag, excluding RCs from
auto-update channels, and the promotion path from an RC to the final tag — are
[RC-02 (issue #446)](https://github.com/DrewBrunning/mycorrhizal-crm/issues/446),
not this page. `release.yml` today accepts the RC tag *format* but explicitly
refuses to run its (final-release) publication steps for one until #446 lands.

## Where the version comes from

**The git tag is the single source of the version.** Everything else is derived
from it at build time; there are no hand-edited version constants on the release
path.

| Derivation | Mechanism |
|---|---|
| Backend `buildinfo.Version` (`/health`, the About dialog) | `-ldflags -X mycorrhizal/buildinfo.Version=…`, set from the tag in `Dockerfile` and `backend/Makefile`; see `backend/buildinfo/buildinfo.go` |
| Docker image tags (`…:1.2.3`, `…:1.2`) | `docker/metadata-action` `type=semver`, `value` = the release tag, in `.github/workflows/docker-publish.yml` |
| Android `versionName` | `docker-publish.yml` "Compute release version" strips the leading `v` and passes `-PMYCORRHIZAL_VERSION_NAME` to Gradle |
| Android `versionCode` | see [below](#android-versioncode) |

### Non-authoritative placeholders

Two version-shaped literals exist in the tree and are **not** sources of truth.
They are placeholders for non-release builds and are overridden on the release
path:

- `frontend/package.json` `"version": "0.1.0"` — the frontend's shipped version
  is `VITE_APP_VERSION`, injected at image build from `APP_VERSION` (the tag).
  The `package.json` field is inert.
- `MycorrhizalAndroidApplicationPlugin.kt` `versionCode = … ?: 1` /
  `versionName = … ?: "0.1.0"` — the fallbacks for a local or manual
  (`android-apk-build.yml`) build. A release build always passes the real
  values.

`versionpolicy.TestKnownVersionPlaceholdersUnchanged` pins both. If either is
turned into a real version number, that test fails — the prompt to update this
section, not to grow a second version source.

### Android `versionCode`

Android refuses an in-place upgrade unless the new APK's `versionCode` is
strictly greater than the installed one, so the rule must be monotonic
release-over-release:

**`versionCode = 1000 + GITHUB_RUN_NUMBER`** — the run number of the
`docker-publish.yml` run that builds the release APK.

`GITHUB_RUN_NUMBER` increases by one on every run of that workflow and never
resets, so it is strictly increasing across releases by construction. The `+1000`
offset keeps every release above `1`, the static `versionCode` that every
non-release build (local, `android-apk-build.yml`) carries. The rule deliberately
does **not** parse `MAJOR.MINOR.PATCH` out of the tag: `v1.0.0` and
`v1.0.0-rc.1` would otherwise collide on one `versionCode`.

The runtime assertion that release *N+1*'s APK actually installs over release
*N*'s is [ANDROID / issue #527](https://github.com/DrewBrunning/mycorrhizal-crm/issues/527)
(the APK release gate), not this page.

## Version numbers and the supported floors

Two floors are expressed as versions and therefore depend on this policy:

- **Supported-upgrade floor — `v0.6.0`.** In-place upgrade is supported from
  here; below it the server refuses to migrate. Canonical statement:
  [upgrade compatibility](upgrade-compatibility.md) (issue #529). The floor
  moves only at a MAJOR bump, which is itself a breaking change.
- **Client/server compatibility floor.** The Android app declares the oldest
  server it will talk to; a server may declare the oldest client it accepts.
  Neither has a floor above "every released version" today. Canonical
  statement: [client/server compatibility policy](client-compatibility-policy.md)
  (ANDROID-01, issue #478).

## Stray and unconstrained tags

The repository inherited a `v1.7.0` tag from upstream meerkat-crm in some local
checkouts (issue #545). `origin` never carried it, and no automation on `main`
resolves "the highest tag" or runs `git describe`, so it never had a code path
to do harm — #545 closed with that finding and a forward rule, restated here as
policy:

- **Any tag query added in future must be constrained.** A `git describe`, a
  changelog range, a "previous release" lookup for a `versionCode` diff — each
  must pass `--match 'v[0-9]*'` (or tighter) and a comment saying why. An
  unconstrained "latest tag" is a defect.
- **A `v1.x` tag means MAJOR = 1**, which only happens at the `1.0.0` stability
  contract. An accidental `v1.something` is caught at review, and the
  `validate-tag` gate still requires it to be well-formed — but the gate cannot
  know MAJOR was unintentional, so the human check is the real guard here.

## How a release is cut

`release.yml` (`workflow_dispatch`, one input: the version) does the whole cut:
register the schema fixture, commit it to `main`, push the tag, and let
`docker-publish.yml` build and sign everything. The full description, and the
verification commands for the published artifacts, are in
[Verifying release artifacts](security/release-verification.md).

## Document consistency

- `versionpolicy.TagPattern` (`backend/internal/versionpolicy/tagpattern.go`) is
  the one authoritative tag-format expression.
  `versionpolicy.TestWorkflowsAndDocUseCanonicalPattern` asserts the identical
  string appears in this document, `release.yml`, and `docker-publish.yml`;
  `versionpolicy.TestGatePatternAcceptsAndRejects` compiles the copy embedded in
  `docker-publish.yml` and checks it accepts and rejects the right tags.
- `versionpolicy.TestKnownVersionPlaceholdersUnchanged` pins the two
  non-authoritative version placeholders named above.
- These run in the normal backend `go test ./...`, so the published policy
  cannot drift from the gate or the code without a red build.
