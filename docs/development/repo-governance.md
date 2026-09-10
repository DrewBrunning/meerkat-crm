---
title: Repository governance
parent: Development
nav_order: 9
---

# Repository governance

**This is the canonical statement of the repository's protection settings (#508).** Branch and
tag rulesets live in GitHub's UI, where they are invisible to review and can be changed
silently. This page — plus the committed desired-state in
[`.github/rulesets/`](https://github.com/DrewBrunning/mycorrhizal-crm/tree/main/.github/rulesets)
— is the source of truth an admin applies from, and
[`.github/workflows/governance-drift.yml`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/workflows/governance-drift.yml)
diffs the live rulesets against it weekly.

`v0.7.0`'s premise is that mandatory gates cannot be bypassed. A gate enforced only inside a
workflow can be bypassed by pushing around the workflow, so these settings are part of release
integrity, not a separate administrative concern.

This page is contributor- and maintainer-facing; the `go run` / `gh api` blocks assume a repo
checkout and the GitHub CLI.

## The `main` branch

Two rulesets, both `enforcement: active`, on `refs/heads/main`:

- **`main-hard-checks`** ([`.github/rulesets/main-hard-checks.json`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/rulesets/main-hard-checks.json)) —
  `deletion` and `non_fast_forward` blocked, **no bypass actors**: `main` cannot be deleted or
  force-pushed by anyone.
- **`main-protection`** ([`.github/rulesets/main-protection.json`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/rulesets/main-protection.json)) —
  every change to `main` goes through a pull request (`squash` or `rebase` merge only), with
  `required_linear_history`, `dismiss_stale_reviews_on_push`,
  `require_extra_approval_for_unattributed_changes`, and the required status checks below.
  Bypass: the repo Admin role and the release GitHub App (which pushes the release-registration
  commit).

`required_approving_review_count` is `0` — this is a solo project; the value of the PR rule here
is the required checks and the linear-history / squash constraints, not human review count.

### Required status checks

These are **generated from** [`.github/release-gates.json`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/release-gates.json)
(REL-03, #447): every gate that is `tier: per-pr`, `mandatory: true`, and has a stable check
context. `go run ./cmd/governancecheck` fails the build if `main-protection.json` and the gate
registry disagree, so the branch-protection list can never drift from the gate list.

<!-- governance-required-checks:begin -->

| Check | Reports from |
|---|---|
| `Detect Changes` | `unit-tests.yml` (path-filter job; always green, required so path-skipped suites can be required) |
| `Backend (Go)` | `unit-tests.yml` |
| `Frontend (Vitest)` | `unit-tests.yml` |
| `Run E2E Tests` | `e2e-tests.yml` |
| `Android (Gradle)` | `android-tests.yml` |
| `Android E2E (emulator)` | `android-tests.yml` |
| `Android scan (mobsfscan)` | `sast.yml` |
| `Scan workflows (zizmor)` | `zizmor.yml` |
| `CIS container hardening scan` | `container-hardening.yml` |
| `Docs & security-doc citations` | `unit-tests.yml` (citecheck + depexceptions + deprecations + docscheck + releasegatecheck + governancecheck) |
| `Go server binary is byte-reproducible` | `reproducibility.yml` |
| `codecov/patch/backend` | Codecov |
| `codecov/patch/frontend` | Codecov |
| `codecov/patch/android` | Codecov |

<!-- governance-required-checks:end -->

## Release tags

**`v-tag-protection`** ([`.github/rulesets/tags-v.json`](https://github.com/DrewBrunning/mycorrhizal-crm/blob/main/.github/rulesets/tags-v.json)) —
a **new** ruleset, `target: tag`, `refs/tags/v*`, `enforcement: active`: `update`, `deletion`,
and `non_fast_forward` blocked. Once a `v*` tag exists it is immutable. A `v*` tag is what
triggers `docker-publish.yml`, which makes it a higher-value target than the branch. Creation is
*not* restricted (that is `release.yml`'s job, via the release App); the App is the sole bypass
actor so an emergency retag is possible through the one-dispatch release path and nothing else.

## Protected `release` environment

`docker-publish.yml`'s publishing jobs (`create-release`, `build-and-push`, `build-android-apk`)
declare `environment: release`. Add **required reviewers** to that environment in
*Settings → Environments → release* to gate publication behind a human approval, so a compromised
PR cannot reach the release path even if it reaches CI (#508 action 5, #513). Until reviewers are
configured the environment reference is a no-op — the jobs run unchanged.

## Commit signing

The decision, deliberately (#508 action 3):

1. **DCO on every commit.** Every commit merged here carries a `Signed-off-by:` line certifying
   origin under the project licence; the `Check commit sign-off` status check (`dco.yml`)
   enforces it on every PR. This is attribution, not a cryptographic signature.
2. **The load-bearing paths are already GitHub-Verified.** A squash-merge to `main` is signed by
   GitHub's web-flow key and shows **Verified**. The release-registration commit and every `v*`
   tag are pushed by the release GitHub App, which GitHub also marks **Verified**.
3. **Per-commit GPG/SSH signature is NOT required.** Requiring every contributor commit to carry
   a verified signature is friction disproportionate to a solo hobby project when (1) and (2)
   already cover the paths that reach a release. Revisit this if the project gains multiple
   maintainers or an external contributor base — a `required_signatures` rule on
   `main-protection` is the switch.

## Workflow token permissions

Continuing what is already in place (#315):

- **Every workflow** sets `permissions: contents: read` at the top level (all 32; a change that
  adds a workflow without one should be caught in review). A job elevates only what it needs.
- **`docker-publish.yml`** is the only workflow that elevates meaningfully: `create-release`
  gets `contents: write` (the Release), `build-and-push` gets `packages: write` + `id-token:
  write` + `attestations: write` (push + keyless sign + provenance), `build-android-apk` gets
  `contents: write` + `id-token: write` + `attestations: write`. `validate-tag`, `release-gate`,
  `scan`, and `verify-release-assets` stay read-only.
- **No long-lived credential** except `RELEASE_APP_PRIVATE_KEY`, which mints a token scoped to
  `contents: write` only, used by one `workflow_dispatch`-only workflow with no pull-request
  path. Every signing operation uses OIDC-federated Sigstore (`id-token: write`), never a stored
  key.

## Applying and verifying

Apply the branch rulesets (find each `<id>` with `gh api /repos/DrewBrunning/mycorrhizal-crm/rulesets`):

```sh
# assumes: GitHub CLI, admin on the repo
gh api --method PUT /repos/DrewBrunning/mycorrhizal-crm/rulesets/<main-protection-id> \
  --input .github/rulesets/main-protection.json
gh api --method PUT /repos/DrewBrunning/mycorrhizal-crm/rulesets/<main-hard-checks-id> \
  --input .github/rulesets/main-hard-checks.json
# tags-v does not exist yet -- create it:
gh api --method POST /repos/DrewBrunning/mycorrhizal-crm/rulesets \
  --input .github/rulesets/tags-v.json
```

Then verify the protections actually hold (#508 action 7) — each of these must be **refused**:

```sh
# assumes: a clean checkout of main, GitHub CLI
git commit --allow-empty -m "probe" && git push origin main         # -> rejected (PR required)
git push --force origin main                                        # -> rejected (non_fast_forward)
git tag -f v0.6.12 && git push --force origin refs/tags/v0.6.12     # -> rejected (tag update blocked)
```

and confirm a PR whose required checks are red cannot be merged.

`governance-drift.yml` is the standing alarm if any of the live rulesets later diverge from
`.github/rulesets/`. Reading rulesets over the API needs `Administration: read`, which the
built-in `GITHUB_TOKEN` cannot be granted — set an optional repo **secret
`GOVERNANCE_READ_TOKEN`** (a fine-grained PAT scoped to this repo with *Administration: read*) to
enable the `ruleset-drift` job; without it that job records a note and the `cosign-identity-probe`
job still runs. The workflow is `continue-on-error`: a diff is a review prompt, not a build
failure, because a maintainer may adjust a setting deliberately and then reconcile the JSON.
