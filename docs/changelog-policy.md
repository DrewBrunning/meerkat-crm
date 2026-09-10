---
title: Changelog policy
nav_order: 13
---

# Changelog policy

**This is the canonical changelog statement (REL-05, issue #449).** It defines
what the changelog *is* for this project, how the "what changed" list is
produced, where the operator-facing part comes from, when a change must carry
one, and where the record begins. `.github/release.yml`, `cmd/releasenotes`, and
the `changelog-note` job in `.github/workflows/unit-tests.yml` are defined
against this document; a change here is a change to those, and vice versa (see
[Document consistency](#document-consistency)).

This page is contributor- and maintainer-facing. What an operator needs — *does
this upgrade require anything of me?* — is answered on each
[GitHub Release](https://github.com/DrewBrunning/mycorrhizal-crm/releases), in
its **Upgrade notes** section.

## What the changelog is

There is **no in-repo `CHANGELOG.md`**. The changelog is the **GitHub Release
notes**, assembled from two sources:

1. **What changed** — GitHub's own release-notes generator
   (`generate_release_notes`), turned into labelled sections by
   `.github/release.yml`. One section per category, populated from the labels on
   the pull requests merged since the previous release.
2. **Operator notes** — an **Upgrade notes** section, and a **Breaking changes**
   section when applicable, **hand-written in each pull request's description**
   and harvested at release time by `cmd/releasenotes` into the top of the
   release body.

The generated list answers *what changed*; only a human can answer *what an
operator must do about it*, which is why part 2 is never generated from a diff.

## Categories

`.github/release.yml` defines these sections, each filled by a PR label:

| Section | Label |
|---|---|
| Security | `security` |
| Breaking Changes | `breaking-change` |
| Deprecated | `deprecation` |
| Added | `enhancement` |
| Fixed | `bug` |
| Dependencies | `dependencies` |
| Documentation | `documentation` |
| Other Changes | `*` (catch-all — every merged PR appears somewhere) |

The `breaking-change` and `deprecation` labels are created when the first such
change ships; until then those sections simply stay empty. The **Breaking
changes** operator section (part 2) is harvested from PR *bodies* and does not
depend on the label.

`Deprecated` entries are also recorded in
[`deprecations.md`](deprecations.md) (MAINT-01, issue #490); `Breaking Changes`
are classified by [`breaking-change-policy.md`](breaking-change-policy.md)
(MAINT-02, issue #491). This page does not restate those.

## When a pull request must carry operator notes

A PR needs an `## Upgrade notes` block in its description when it does any of:

- adds, renames, retypes, or drops a **database migration**;
- adds, renames, or changes the default or meaning of a **configuration
  variable**;
- changes **observable behavior** an operator or client could rely on;
- **deprecates** anything on a covered surface;
- otherwise requires **action before or after upgrading**.

Add `## Breaking changes` as well when the change is breaking under
[`breaking-change-policy.md`](breaking-change-policy.md).

If a PR touches one of the tracked paths (`backend/database/migrations/**`,
`backend/config/config.go`, `.env.example`) but genuinely needs no operator
note, put `no-changelog: <reason>` in the PR description. The `changelog-note`
CI job fails a PR that touches those paths with neither an `## Upgrade notes` /
`## Breaking changes` heading nor a `no-changelog:` line.

Everything else — internal refactors, test-only changes, most bug fixes — needs
nothing beyond the right label.

## Upgrade notes are always answered

`cmd/releasenotes` always emits an **Upgrade notes** section. When no PR in the
range contributed one, it emits:

> None — no action required for an instance already on ≥ v0.6.0.

So every release states the operator position explicitly rather than by
omission.

## Skip-version upgrades

Self-hosted operators skip versions — `v0.6.0 → current` directly is supported
([`upgrade-compatibility.md`](upgrade-compatibility.md), issue #529). Because
every release body carries an **Upgrade notes** section, the accumulated
operator actions across a range are read release-by-release down the
[Releases page](https://github.com/DrewBrunning/mycorrhizal-crm/releases). The
one known cross-version upgrade defect (a crash upgrading into `v0.6.1`–`v0.6.8`
from `≤ v0.6.0` with audit history, fixed in `v0.6.9`) is documented in
[`upgrade-compatibility.md`](upgrade-compatibility.md) and repeated in the
affected releases' notes.

## Versions and issue links

The version a set of notes belongs to is the **git tag**
([`versioning-policy.md`](versioning-policy.md), REL-01). Publication and note
assembly happen together: the harvest runs inside the release build
(`docker-publish.yml`, `create-release`), so a release cannot be published
without its notes. Each generated line links its PR, and each harvested block is
prefixed with its `#NNN` link — which is how the `0.9.x` release-candidate
traceability requirement (issues #503/#504), *every change traceable to a
finding*, becomes checkable rather than aspirational.

## Where the record begins

`v0.6.0` — the [supported-upgrade floor](upgrade-compatibility.md) (issue #529).
Releases from `v0.6.0` onward carry an **Upgrade notes** section (the
`v0.6.0`–`v0.6.12` releases were backfilled with one when this policy landed).
Earlier releases have generated notes only; reconstructing twenty pre-floor
releases is not worth it, and nothing supported upgrades from below the floor in
one hop anyway.

## Document consistency

- `.github/release.yml`'s category set is pinned by
  `backend/internal/releasenotes` (`RequiredCategories`);
  `TestReleaseConfigHasRequiredCategories` fails the build if a section is
  dropped or the `*` catch-all loses its label.
- `cmd/releasenotes` (harvest + assembly) is covered by
  `backend/internal/releasenotes` unit tests.
- `changelog-note` in `.github/workflows/unit-tests.yml` enforces the
  operator-note requirement on every pull request.

These run in the normal `go test ./...` and CI, so the policy cannot drift from
the tooling without a red build.
