# Verifying Release Artifacts

Every tagged release (`docker-publish.yml`) publishes three Docker images and a signed Android
APK. This page is the operator-facing "how do I check this is genuine" guide: exact commands to
verify a downloaded binary/container actually came from this repository's CI, unmodified. No
supply-chain background is assumed.

It documents what's already in place (issues #272, #328) — it introduces no new signing. See
`docs/security/asvs-l2.md` (rows 1.14.2, 10.3.1, 14.2.5) for how this fits the ASVS checklist.
OpenSSF Scorecard's `Signed-Releases` check (issue #355) is satisfied because the release carries
the APK's cosign bundle as an asset (`mycorrhizal-apk.sigstore.json`). That check is purely
filename-based: it scans release assets for signature extensions (`.asc`, `.sig`, `.minisig`,
`.sigstore`, `.sigstore.json`, ...) and ignores everything else — the attestations API, the OCI
registry, and workflow artifacts.

## How a release is cut

`release.yml` (`workflow_dispatch`, one input: the version, e.g. `v0.6.6`) is the whole release
process. It:

1. registers the release in `backend/internal/schemafixture/releases.go` and regenerates its
   committed schema dump (`cmd/genschema`), failing if any *other* dump changes — the frozen,
   append-only migration chain must reproduce byte-identical;
2. runs the schemafixture + genschema + releaselist test gates;
3. commits those two files to `main` and pushes a **lightweight** tag at that commit.

The tag push triggers `docker-publish.yml`, which builds and signs everything listed below and
creates the GitHub Release. That hand-off works only because the push uses a **GitHub App token**
(repo variable `RELEASE_APP_ID` + secret `RELEASE_APP_PRIVATE_KEY`, App on `main`'s
branch-protection bypass list) — a tag pushed with the default `GITHUB_TOKEN` cannot trigger
another workflow. The App token is scoped to `contents: write` and is used by this one
`workflow_dispatch`-only workflow; there is no PR-triggered path to it.

Because the tag points at a real commit on `main` (the one carrying the dump), the
`schema-fixture-gate` in `docker-publish.yml` passes and source↔release correspondence (below)
is exact — there is no post-review "move the tag" step.

If `docker-publish.yml` fails after the tag is pushed, re-run it from its own **Run workflow**
button with the `tag` input; do not re-dispatch `release.yml` (it refuses an existing tag).

## What's attached to a release, and what it proves

| Artifact | Signal | Proves | Expires? |
|---|---|---|---|
| Docker images (all-in-one, `-backend`, `-frontend`) | cosign keyless signature (pushed to the registry alongside the image) | The image was signed by this repo's `docker-publish.yml` via GitHub's OIDC identity | No — lives in the registry as long as the image does |
| Docker images | SLSA build provenance, two forms: GitHub-native attestation + buildkit in-toto attestation | Which commit/workflow run produced this exact digest | No — GitHub attestation is permanent; buildkit provenance lives in the registry |
| Docker images | SBOM (SPDX, buildkit-embedded referrer) | Full dependency list for the exact image you pulled | No — lives in the registry |
| Docker images | Standalone signed SBOM (SPDX + CycloneDX, cosign-signed) | Same dependency list, as a portable file + signature | **Yes — 30-day GitHub Actions artifact retention** |
| Android release APK | Keystore signature (`SIGNING_*` secrets) | The APK is installable and matches every other release signed with the same key (Android's own trust mechanism) | No |
| Android release APK | GitHub-native SLSA build provenance | Which commit/workflow run built this exact APK; shows a "Verified" badge on the Release page | No — permanent |
| Android release APK | cosign keyless co-signature (additive, does not replace keystore signing) | Independent Sigstore-backed verifier on top of the GitHub attestation; also what Scorecard's `Signed-Releases` check sees | No — attached to the Release as `mycorrhizal-apk.sigstore.json` (a copy is also kept as a 30-day workflow artifact) |

The one "expires" row is a workflow *run* artifact (`actions/upload-artifact`), not a GitHub
Release asset — it is only downloadable from the specific `docker-publish.yml` run's Actions
page, and only for 30 days after that run. For a release older than that, skip that check and
rely on the permanent ones (cosign image signature, both provenance forms, the APK's cosign
bundle on the Release, source↔release correspondence below) — they cover the same ground.

## Prerequisites

Install two CLIs:

```sh
# cosign — signature and SBOM verification
brew install cosign            # macOS
# or: go install github.com/sigstore/cosign/v2/cmd/cosign@latest
# or download a release binary: https://github.com/sigstore/cosign/releases

# GitHub CLI — attestation verification
brew install gh                # macOS
# or: https://github.com/cli/cli#installation
```

`gh attestation verify` works unauthenticated against this public repo, but `gh auth login` first
avoids GitHub's unauthenticated API rate limit if you're verifying more than a couple of times in
a row.

## Verifying a Docker image

Replace `<TAG>` with the release you're deploying, e.g. `0.6.1`. The published
image tag drops the leading `v` from the git tag (`v0.6.1` → `0.6.1`), so
`docker pull …:v0.6.1` would 404.

**1. Pull the image and pin the digest** (a tag is mutable; the digest is what's actually signed):

```sh
docker pull ghcr.io/drewbrunning/mycorrhizal-crm:<TAG>
DIGEST=$(docker inspect --format='{{index .RepoDigests 0}}' ghcr.io/drewbrunning/mycorrhizal-crm:<TAG>)
echo "$DIGEST"   # ghcr.io/drewbrunning/mycorrhizal-crm@sha256:...
```

(For the split images, use `ghcr.io/drewbrunning/mycorrhizal-crm-backend:<TAG>` or
`...-frontend:<TAG>` instead.)

**2. Verify the cosign signature:**

```sh
cosign verify "$DIGEST" \
  --certificate-identity-regexp 'https://github.com/DrewBrunning/mycorrhizal-crm/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

A successful verification prints the signed payload and exits `0`. A tampered or unsigned image
fails with `Error: no matching signatures`.

**3. Verify SLSA build provenance** (GitHub-native attestation — shows which commit and workflow
run produced this digest):

```sh
gh attestation verify "oci://$DIGEST" -R DrewBrunning/mycorrhizal-crm
```

**4. Inspect the buildkit-embedded provenance and SBOM directly** (a second, independent source
for the same facts, stored as OCI referrers in the registry rather than GitHub's attestation
store):

```sh
docker buildx imagetools inspect ghcr.io/drewbrunning/mycorrhizal-crm:<TAG> --format '{{ json .Provenance }}'
docker buildx imagetools inspect ghcr.io/drewbrunning/mycorrhizal-crm:<TAG> --format '{{ json .SBOM }}'
```

The provenance JSON's `predicate.invocation` / `predicate.materials` fields name the exact source
commit — cross-check it against the tag as described below.

## Verifying the Android release APK

Download `app-release.apk` from the release's GitHub Release page
(`https://github.com/DrewBrunning/mycorrhizal-crm/releases/tag/<TAG>`).

**1. SLSA build provenance** (works for every release, indefinitely):

```sh
gh attestation verify app-release.apk -R DrewBrunning/mycorrhizal-crm
```

The Release page itself also shows a "Verified" badge next to the asset when this attestation is
present.

**2. cosign co-signature** (additional Sigstore-backed signal): download
`mycorrhizal-apk.sigstore.json` from the release's GitHub Release page (attached as a release
asset; a copy is also kept as a 30-day `apk-cosign-signature` workflow artifact on the
`docker-publish.yml` run). It is cosign v3's standardized bundle — cert, signature, and
transparency log entry in one file — then:

```sh
cosign verify-blob \
  --bundle mycorrhizal-apk.sigstore.json \
  --certificate-identity-regexp 'https://github.com/DrewBrunning/mycorrhizal-crm/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  app-release.apk
```

**3. Installability** — the keystore signature that actually lets Android install/upgrade the
APK is separate from both of the above and is checked automatically by the OS (or by `apksigner
verify app-release.apk` if you want to confirm it yourself); it's what proves this release was
built with the same signing key as every prior release, so an update can't be substituted by
someone without that key.

## Verifying source↔release correspondence

The three checks above establish "this artifact was produced by this repo's CI." A separate,
useful question is "which commit, exactly" — and whether the thing you're *running* matches the
tag you think you deployed.

Every image is stamped at build time with the commit it was built from
(`backend/buildinfo/buildinfo.go`), exposed on the running instance:

```sh
curl -s https://your-instance.example.com/health | jq '{version, commit, build_date}'
```

Compare the `commit` field (a 12-character prefix) against the tag's actual commit on GitHub:

```sh
git ls-remote https://github.com/DrewBrunning/mycorrhizal-crm.git refs/tags/<TAG>
```

(Release tags are lightweight and point directly at a commit on `main` — the one `release.yml`
pushed, carrying that release's schema dump. `refs/tags/<TAG>^{}` still works and resolves to the
same commit.) The two should share the same prefix. This same commit also appears in the
provenance JSON pulled in step 4 above, so all three — the running binary, the git tag, and the
signed provenance — should agree.

## Verifying a standalone SBOM

Both the per-release SBOMs (`docker-publish.yml`, one per image) and the continuous main-branch
SBOM (`syft-sbom.yml`, one per merge to `main`) are uploaded as cosign-signed workflow artifacts —
**30-day retention**, unlike the APK cosign bundle above, which is attached to the Release
permanently. From the relevant workflow run's Actions page, download the SBOM artifact bundle
(`sbom-signatures[-backend|-frontend]` for a release, `sbom-signatures` for a main-branch SBOM)
and verify:

```sh
cosign verify-blob \
  --bundle sbom.spdx.json.sigstore.json \
  --certificate-identity-regexp 'https://github.com/DrewBrunning/mycorrhizal-crm/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  sbom.spdx.json
```

Past the 30-day window, use the registry-embedded SBOM instead (step 4 under "Verifying a Docker
image" above) — it's the same document, just without a portable standalone signature.

## Reproducibility

The checks above prove an artifact came from this repo's CI. A related question — *can I rebuild
the source and get the same bytes?* — has its own page:
[Reproducible builds](reproducible-builds.md) (REL-04, issue #448). In short: the Go server
binary is byte-reproducible and gated every PR by `.github/workflows/reproducibility.yml`
(`-trimpath -buildvcs=false`, `SOURCE_DATE_EPOCH`); the `linux/amd64` image's config + layer
digests are double-build-compared every run (not yet a hard gate); the multi-arch manifest
digest and the signed Android APK are not bit-reproducible by design, and provenance is the
mitigation. That page carries the exact rebuild-and-compare commands and the full per-artifact
table.
