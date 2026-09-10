# Reproducible builds

This is the REL-04 (issue #448) per-artifact reproducibility statement. The milestone criterion
is deliberately permissive — artifacts must be **reproducible, or their reproducibility
limitations explicitly documented** — and this page is that document. It sits next to
[Verifying release artifacts](release-verification.md), which covers signatures, provenance, and
source↔release correspondence; this page covers *"can I rebuild it and get the same bytes."*

The posture: reproducible where it is cheap, documented where it is not, provenance everywhere.

## Per-artifact status

| Artifact | Reproducible? | How it is kept so, or why it is not | Source revision |
|---|---|---|---|
| **Go server binary** | **Yes — byte-identical** | Pinned Go toolchain (`backend/go.mod` `toolchain`), `CGO_ENABLED=0` pure-Go build, `-trimpath` (drops the builder's absolute paths), `-buildvcs=false` (keeps the embedded VCS stamp out — the commit is carried by an `-X` ldflag instead), `SOURCE_DATE_EPOCH` honoured for any embedded time. Gated every PR by `.github/workflows/reproducibility.yml` → `go-binary`, which builds it twice from two different paths and `cmp`s the result. | `/health` → `commit` (`backend/buildinfo/buildinfo.go`), a 12-char prefix; cross-check per [release-verification.md](release-verification.md#verifying-sourcerelease-correspondence). |
| **All-in-one image** (`linux/amd64`) | **Config + layer digests compared every run; not yet a hard gate** | Base images pinned **by digest** (`golang:1.27.1-alpine@sha256:…`, `node:26-alpine@sha256:…`, `alpine:3.24@sha256:…`), apk packages pinned to exact `=version-rN`, `-trimpath -buildvcs=false` on the Go build, and `SOURCE_DATE_EPOCH` (the release commit's committer date) passed to BuildKit so every layer/config timestamp is rewritten to it. `reproducibility.yml` → `all-in-one-image` double-builds it and records the config + layer-digest comparison in the job summary every run; it is `continue-on-error` until it has been green across enough `main` runs to block a merge on (the `apk add` layer and BuildKit metadata are the unproven part). | `org.opencontainers.image.revision` label (full SHA, set explicitly from the checked-out tag — not `github.sha`), and `/health` from the running backend. |
| **Split `-backend` / `-frontend` images** (`linux/amd64`) | **Same posture as the all-in-one image** | Same Dockerfiles, same pins, same `SOURCE_DATE_EPOCH`. Not separately built in CI — the inputs are a strict subset of the all-in-one image's. | `org.opencontainers.image.revision` label. |
| **Published multi-arch manifest** (`amd64` + `arm64`) | **No — not asserted bit-for-bit** | `arm64` is cross-built under QEMU emulation, and the pushed artifact is an OCI **manifest list** whose top-level digest folds in per-arch descriptors and registry-side compression. Making the list digest deterministic across runners is disproportionate effort here. **Mitigation:** each arch image still carries buildkit + GitHub SLSA provenance and a cosign signature naming the exact source commit and workflow run. | buildkit provenance / GitHub attestation (`gh attestation verify oci://…`). |
| **Frontend bundle** (`build/`) | **Effectively yes**, not independently gated | `yarn install --frozen-lockfile` against the committed `frontend/yarn.lock` under the digest-pinned `node:26-alpine`, then Vite's content-hashed output. Ships **inside** the frontend / all-in-one images, so its bytes are covered by those images' layer-digest check. | The containing image's `org.opencontainers.image.revision`; the bundle also carries `VITE_APP_VERSION` (the release version) for the stale-client check (`frontend/src/staleClient/version.ts`). |
| **Android release APK** | **No — not bit-reproducible by design** | The APK is signed with the release keystore (`SIGNING_*` secrets); the signature block differs every build by construction. R8 shrinking, dexing, and zipalign are also not guaranteed identical across Android toolchain patch versions. **Mitigation:** the keystore signature (Android's own trust mechanism — proves the same key built every release) plus GitHub-native SLSA provenance (`gh attestation verify app-release.apk`) naming the source commit and workflow run. | SLSA provenance predicate (`gh attestation verify …`). |

## Cheap non-determinism that was removed

REL-04 closed these; everything else in the table above was already in place.

- **`-trimpath` and `-buildvcs=false`** — `-trimpath` was absent from all three `go build`
  invocations (`Dockerfile`, `backend/Dockerfile`, `backend/Makefile`), so binaries embedded
  `/app/…` (Docker) or the developer's `$HOME/…` (local `make build`). `-buildvcs=false` makes
  the "no VCS stamp" behaviour explicit (it was only implicit-because-no-`.git` in the Docker
  builds, and *on* for a checkout-based rebuild) so a rebuild from a git checkout still matches.
  Both added.
- **`SOURCE_DATE_EPOCH`** — was unset in `docker-publish.yml`, so image layer and config
  timestamps were the workflow run's wall-clock. Now set to the release commit's committer date.
- **OCI identity labels** — `docker-publish.yml` passed `metadata-action`'s default labels
  through, so `org.opencontainers.image.revision` was `github.sha` (the *branch* head on a manual
  recovery dispatch, not the tag) and `.created` was the run time. Both are now set explicitly
  from the checked-out tag, matching the reasoning already applied to the image `tags:`.

Base images (digest-pinned) and apk packages (exact-version-pinned) were already deterministic.

## Known, accepted limitations

- **The multi-arch manifest digest** — see the table. Provenance + signature is the mitigation.
- **The Android APK** — not bit-reproducible by design; signature + provenance is the mitigation.
- **Gradle dependency locking** — the Android build emits no `gradle.lockfile`, which is also why
  `license-compliance.yml` cannot enumerate Gradle licenses (`.github/filters.yaml`). Enabling it
  is **deferred to COMPAT-03 (issue #474)**, the dependency-upgrade-policy work, where it belongs
  alongside the other ecosystems' lockfile decisions. The APK's SLSA provenance already answers
  "what produced this," which is the reproducibility-adjacent question that matters most here.
- **`npm` install pinning (issue #331)** — resolved: PR #737 removed the dead
  `if … elif … else npm install` branch from all three Dockerfiles, leaving only
  `yarn install --frozen-lockfile`. No residual gap.

## Rebuild it yourself

The signature / provenance / SBOM verification commands are in
[Verifying release artifacts](release-verification.md). To additionally check that the **source
rebuilds to the same bytes**:

**Go server binary** (needs the Go toolchain from `backend/go.mod` and a checkout of the release
tag):

```sh
# assumes: Go toolchain per backend/go.mod, run from a clean checkout of the tag
cd backend
EPOCH=$(git log -1 --format=%ct HEAD)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 SOURCE_DATE_EPOCH="$EPOCH" \
  go build -trimpath -buildvcs=false \
  -ldflags "-X mycorrhizal/buildinfo.Version=<TAG-without-v> -X mycorrhizal/buildinfo.Commit=$(git rev-parse --short=12 HEAD) -X mycorrhizal/buildinfo.BuildDate=$(date -u -d @"$EPOCH" +%Y-%m-%dT%H:%M:%SZ)" \
  -o /tmp/mycorrhizal-rebuilt .
```

Then extract the binary from the published image and compare:

```sh
cid=$(docker create ghcr.io/drewbrunning/mycorrhizal-crm:<TAG-without-v>)
docker cp "$cid":/app/mycorrhizal /tmp/mycorrhizal-published
docker rm "$cid"
sha256sum /tmp/mycorrhizal-rebuilt /tmp/mycorrhizal-published   # the two hashes match
```

**All-in-one image** (`linux/amd64`, needs Docker with Buildx; assumes a Go toolchain is *not*
required — the build is fully containerised):

```sh
SOURCE_DATE_EPOCH=$(git log -1 --format=%ct HEAD) docker buildx build \
  --platform linux/amd64 --no-cache \
  --build-arg APP_VERSION=<TAG-without-v> \
  --build-arg APP_COMMIT=$(git rev-parse HEAD) \
  --build-arg APP_BUILD_DATE=$(date -u -d @"$(git log -1 --format=%ct HEAD)" +%Y-%m-%dT%H:%M:%SZ) \
  --output type=oci,dest=/tmp/rebuilt.tar,rewrite-timestamp=true \
  -f Dockerfile .
# compare the image config + layer digests inside /tmp/rebuilt.tar against the published
# linux/amd64 image (`docker buildx imagetools inspect …`); they are expected to match, and the
# reproducibility.yml job checks exactly this on every run

```

`.github/workflows/reproducibility.yml` runs both of these comparisons on every relevant PR, on
every push to `main`, and weekly; each run's job summary is a recorded double-build result.
