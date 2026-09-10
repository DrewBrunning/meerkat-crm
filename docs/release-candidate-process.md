---
title: Release-candidate process
nav_order: 14
---

# Release-candidate process

**This is the canonical release-candidate policy (REL-02, issue #446).** `0.9.x` is a
release-candidate series by design — every change traceable to an RC finding, no new feature
scope, no unnecessary architectural change, each revision "progressively smaller and increasingly
boring." This page turns that from prose into mechanics: how an RC is identified and published,
where it is worked on, what may merge into it, how the iteration loop runs, and what ends it.

The tag *identifier* is decided in [`versioning-policy.md`](versioning-policy.md) (RC-01); the
release *workflow* is [`.github/workflows/release.yml`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/workflows/release.yml)
(REL-06, issue #499); the gate list is [`development/release-gates.md`](development/release-gates.md)
(REL-03). This page is only the RC-specific layer on top.

## RC identity and channels

An RC tag is **`vMAJOR.MINOR.PATCH-rc.N`**, `N` from `1` (e.g. `v1.0.0-rc.1`, `v1.0.0-rc.2`, …).
Because a hyphenated identifier **is** a SemVer pre-release, "not a final release" is mechanical
rather than a naming convention someone has to remember:

- `docker-publish.yml`'s `create-release` job sets `prerelease: true` and never `make_latest` for
  an `-rc.` tag.
- The **in-app update check** (`backend/services/update_check.go`) queries GitHub's
  `/releases/latest`, which **excludes pre-releases** — so an RC is never offered as an update.
  The version comparison would rank `v1.0.0-rc.1` above `v0.6.x` if it were ever handed one; the
  pre-release flag is what stops it being handed one.
- **Obtainium** (the Android update path) skips pre-releases by default, so an RC APK is not
  offered to phones automatically.

The RC APK is still built, keystore-signed, `apksigner`-verified, and attached to the Release —
an RC exercises the full APK build/sign path (issue #527). It just does not reach auto-update
channels.

## The `release/vX.Y.0` branch

At **rc.1**, cut a branch **`release/vX.Y.0`** from `main`. From then on:

- **RC tags are cut from `release/vX.Y.0`**, not `main` — dispatch `release.yml` with
  `version: vX.Y.Z-rc.N` and `ref: release/vX.Y.0`.
- **`main` keeps moving.** Unrelated work continues to land on `main` as normal; it is not frozen.
- **Only RC fixes land on `release/vX.Y.0`** (next section).
- On **promotion**, `promote-rc.yml` merges `release/vX.Y.0` back into `main`, so every RC fix
  reaches `main` and the two histories reconverge.

This is enforced, not just documented: **`.github/rulesets/release-branches.json`** (the
`release-branch-protection` ruleset — see [`development/repo-governance.md`](development/repo-governance.md))
requires **the same status checks as `main`**, plus linear history, no deletion, and no
force-push. `backend/internal/governance.CheckReleaseBranchesMatchMain` fails the build if the
`release/*` required-check list ever drifts from `main`'s, so "RC gates match release gates"
(issue #446 action 7) holds by construction.

The per-PR gate workflows (`unit-tests`, `e2e-tests`, `android-tests`, `sast`, `zizmor`,
`container-hardening`, `reproducibility`, `codeql`, `migration-tests`) run on `pull_request` into
`release/**`, so an RC-fix PR is gated identically to a `main` PR. The slow **release-tier**
suites are not per-PR on `main` and are not per-PR here either — `release.yml` dispatches and
awaits all of them when it cuts each RC (they carry `workflow_dispatch`).

## What qualifies as an RC fix — the merge criterion

Every PR into `release/*` must satisfy one of:

1. it **closes an issue labelled `rc-finding`** (`Closes #NNN` / `Fixes #NNN` / `Resolves #NNN`
   in the body); or
2. its body carries an **`rc-chore: <reason>`** line — reserved for version bumps, changelog
   rows, and the promotion merge, which have no finding to point at.

Enforced by the **`RC fix criterion`** check (`.github/workflows/rc-fix.yml`), which runs only on
PRs targeting `release/**` and mirrors the `changelog-note` gate. It is a required check in the
`release-branch-protection` ruleset, so a PR that traces to nothing cannot merge.

Beyond traceability, an RC fix is: **no new feature scope; no architectural change that is not
itself an RC finding.** That judgement sits with the reviewer — the check enforces the paper
trail, not the taste.

## The iteration loop

1. **A finding** — anything wrong with the RC — is filed as an issue, labelled **`rc-finding`**,
   given a **`Severity:`** line ([`issue-classification.md`](issue-classification.md)) and the
   `vX.Y.0` milestone.
2. **The fix** is a PR into `release/vX.Y.0` closing that issue (criterion above).
3. **The next RC** is cut with `release.yml` (`version: vX.Y.Z-rc.(N+1)`, `ref: release/vX.Y.0`).
4. **Traceability is automatic.** Each RC's GitHub Release notes are generated from the merged
   PRs and categorised by [`.github/release.yml`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/release.yml);
   every PR links its `rc-finding` issue. Add a short `## RC changes` section to the release body
   naming the findings this RC closes.
5. Successive RCs get **smaller**. An RC whose diff is larger than its predecessor's, or which
   introduces a new `sev1`/`sev2`, is a signal the series is not converging.

## The exit criterion — when an RC becomes the release

Promote `vX.Y.Z-rc.N` to `vX.Y.Z` when **all** of:

- **No open `rc-finding` issue at `sev1` or `sev2`** against the series. "Release-blocking" is
  defined as `sev1` (data loss / security / auth bypass) or `sev2` (wrong data or silent failure
  with no workaround) per [`issue-classification.md`](issue-classification.md) — not a fresh
  definition invented here.
- The **latest RC introduced only `sev3`/`sev4`/documentation changes** — "increasingly boring".
- **Every mandatory gate is green** on the RC commit (the same battery `release.yml` and
  `docker-publish.yml` run for a final release; see the parity table below).

Then run **`promote-rc.yml`** with `rc_tag: vX.Y.Z-rc.N`.

## Gate parity — RC vs final release

| Step | Final release | Release candidate |
|---|---|---|
| Tag format gate (`validate-tag`) | ✅ | ✅ (pattern accepts `-rc.N`) |
| `citecheck` (security-doc citations) | ✅ hard | ✅ hard |
| `releasegatecheck` (gate registry coherent) | ✅ | ✅ |
| Mandatory `release_gate: true` checks green on the commit | ✅ | ✅ |
| Release-tier suites triggered + awaited | ✅ | ✅ (against `release/*`) |
| `docker-publish.yml` `release-gate` + all `release-internal` jobs | ✅ | ✅ (identical) |
| SLSA provenance + `SHA256SUMS` on the Release | ✅ | ✅ |
| Schema-fixture registration (`SupportedReleases` + dump) | ✅ at tag time | ⏸ deferred to promotion — an RC ships the same schema, and no upgrade is supported *from* an RC |
| ASVS/MASVS §10 re-verification changelog row | ✅ required | ⏸ final-release obligation — the RC still runs `citecheck`; the dated row lands before promotion |
| GitHub Release marked pre-release / not `make_latest` | ❌ | ✅ |

Only the last three rows differ, and each is a deliberate, documented exception.

## Promotion — the same artifact ships

`promote-rc.yml` (`workflow_dispatch`, input `rc_tag`) **copies; it never rebuilds**:

- the three container images are re-tagged **by digest**
  (`docker buildx imagetools create`), so `ghcr.io/…:1.0.0` and `ghcr.io/…:1.0.0-rc.N` resolve
  to byte-identical manifests, plus an additional `cosign` signature carrying the
  `promote-rc.yml` identity;
- every Release asset (APK, cosign bundle, SLSA provenance, SBOMs) is downloaded from the RC
  Release and re-uploaded unchanged, verified against the RC's `SHA256SUMS`;
- the final `vX.Y.Z` git tag is pushed with `GITHUB_TOKEN`, which **does not trigger**
  `docker-publish.yml` — nothing rebuilds;
- the final schema fixture is registered against the RC's tree, and `release/vX.Y.0` is merged
  back into `main`;
- **`promotion-metadata.json`** records `digest_rc == digest_final` for every image — the
  machine-checkable proof that promotion copied rather than rebuilt. `promote-rc.yml` fails if
  any digest differs.

[`security/reproducible-builds.md`](security/reproducible-builds.md) (REL-04) is the *backstop* —
if a digest ever does differ, reproducibility is how you tell whether the difference is benign —
not the mechanism.

## One-time setup for a series

- `gh label create rc-finding --description "A defect found while testing a release candidate" --color BFD4F2`
- Apply `.github/rulesets/release-branches.json` (see [`development/repo-governance.md`](development/repo-governance.md), "Applying and verifying").
- Before `0.9.0`, run the loop end to end once on a throwaway version — cut a practice RC, file a
  finding, cut rc2, promote — so the process is exercised before it is load-bearing (issue #446
  "How to verify").
