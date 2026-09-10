# Deployment security baseline (operator security boundary)

Self-hosting hands most of the security posture to the operator's environment. This doc draws the
line explicitly: the reference topology, the baseline an operator should reach before exposing an
instance to the internet, and — the part that was previously only implied — exactly what this
application does **not** secure. It is the ASVS V1.2 (threat modeling)/V1.14 (security architecture)
deployment evidence; `docs/security/threat-model.md`'s "self-hosted boundary" section states the same
assumption for the whole threat model, this doc is its concrete, operator-facing checklist.

| | |
|---|---|
| **Last updated** | 2026-08-26 (issue [#417](https://github.com/DrewBrunning/mycorrhizal-crm/issues/417)) |
| **Scope** | The shipped all-in-one Docker image (`Dockerfile`, `docker-compose.yml`) and everything outside its container boundary: host OS, Docker daemon, reverse proxy, network, DNS, backup storage. |
| **Companion docs** | `docs/deployment.md` (setup/upgrade/backup *procedures*), `docs/security/threat-model.md` (assets/actors/gating decisions), `docs/security/asvs-l2.md` (per-control checklist — V1.2.1/1.2.2, V1.14.1/1.14.5 cite this doc), `docs/security/data-retention-lifecycle.md` (retention) |

Complements issue [#362](https://github.com/DrewBrunning/mycorrhizal-crm/issues/362) (CIS container
hardening, enforced in CI), [#364](https://github.com/DrewBrunning/mycorrhizal-crm/issues/364)
(security headers/HSTS), and [#377](https://github.com/DrewBrunning/mycorrhizal-crm/issues/377)
(threat model) — this doc is mostly the write-up of guarantees those tickets already built, plus the
explicit non-goals none of them stated.

## Reference topology

```
Internet
   │  HTTPS — TLS terminated here. The app never terminates TLS itself; it has no
   │  certificate material and expects to be reached over plain HTTP from a
   │  trusted proxy hop (`docs/deployment.md:32-45`).
   ▼
Operator-managed reverse proxy (nginx, Caddy, Traefik, …)
   — owns: TLS certificates/renewal, DNS, firewall/exposed ports, request
     logging, rate limiting at the edge (optional, in addition to the app's own).
   │  plain HTTP, private Docker network or localhost only
   ▼
mycorrhizal container  (docker-compose.yml)
   ├─ nginx :8080         published as ${FRONTEND_PORT:-7300} (docker-compose.yml:23-24)
   │                      serves the SPA, proxies /api, /carddav, /caldav, /.well-known/carddav
   └─ backend :8081       loopback-only inside the container, never published
                          (Dockerfile ENV PORT=8081; docker/nginx.conf proxies to 127.0.0.1:8081)
   │
   ▼
Persistent volumes — host bind mounts (docker-compose.yml:19-22)
   ├─ DATA_PATH        → /app/data              (SQLite db + WAL, default ./data)
   ├─ PHOTOS_PATH      → /app/static/photos     (default ./photos)
   └─ ATTACHMENTS_PATH → /app/static/attachments (default ./attachments)
   │
   ▼
Operator-managed backup storage — `make backup` / `VACUUM INTO` snapshot + `rsync`'d
photos/attachments, moved off-host however the operator chooses (`docs/deployment.md`
"Backups" and "Backup confidentiality & retention")
```

There is no separate database service or port in this topology — SQLite is embedded in the backend
process, so "don't expose a database port" is true by construction here, not a setting an operator
can forget. If a future change ever introduces an external database, this diagram is the place to
add its own private-network hop.

## Recommended baseline

Each row states what the app/image already does versus what remains the operator's action. Rows
already fully satisfied by the shipped image are the ones you get for free by using
`docker-compose.yml` as published; the rest need an operator decision.

| Control | What the app/image does today | What the operator must still do |
|---|---|---|
| **HTTPS + HSTS** | Emits `Strict-Transport-Security` at both layers, gated on the same signal so they can't disagree: the backend when `COOKIE_SECURE=true` (`backend/middleware/security_headers.go:38-47`), and nginx via `docker/entrypoint.sh` rendering `/etc/nginx/hsts.conf` from the same `COOKIE_SECURE` env var at container start. Never emitted over plain HTTP by default (`docker-compose.yml`'s published port is HTTP). | Terminate TLS at a reverse proxy in front of the published port (example in `docs/deployment.md:19-30`) and set `COOKIE_SECURE=true` + `FRONTEND_URL=https://…` once it's in place. Get a certificate (Let's Encrypt/ACME or your own CA) — the app has no certificate material and cannot obtain one for you. |
| **Secure cookies** | Session cookie is `HttpOnly` always, `Secure` tied to `COOKIE_SECURE`, `SameSite=Lax` (`threat-model.md` Gating decision 5). Boot-time validation refuses `FRONTEND_URL=https://…` with `COOKIE_SECURE=false` (`backend/config/config.go:520-530`) — a misconfigured deployment fails closed instead of shipping an insecure cookie over HTTPS. | Set `COOKIE_SECURE=true` and `COOKIE_DOMAIN` (`.env.example:136,148`) to match your real domain when deploying behind HTTPS. |
| **No DB port exposed** | Not applicable by design — SQLite is embedded in the backend process; there is no separate database service, container, or port to expose or forget. | Nothing — this stays true unless the architecture changes (see the topology diagram's note). If you ever front this with an external database, give it its own private network with no published port. |
| **No Docker socket mounted** | `docker-compose.yml` mounts only the three data volumes (`docker-compose.yml:19-22`) — no `/var/run/docker.sock`. The app has no code path that expects one. | Never add a Docker-socket mount for this container (e.g. to bolt on a self-update/watchtower-style pattern) — that turns any code-execution bug in the app into full host/Docker-daemon compromise. If you want auto-updates, run the updater as a *separate* container/process that itself doesn't need to be reachable from this one. |
| **Non-root container process** | The backend runs as `appuser` (uid/gid 1001 by default, remapped to `PUID`/`PGID` at startup — `docker/entrypoint.sh`), enforced in CI (`docker/cis-hardening.sh` 4.1/4.8 via `.github/workflows/container-hardening.yml`). Setuid/setgid bits are stripped from every installed binary at image build time (`Dockerfile`'s `find … chmod a-s`). PID 1 itself starts as root — a documented, CI-accepted exception (`docker/cis-hardening.ignore` 4.1) so the entrypoint can remap PUID/PGID and supervisord can drop the backend to `appuser` and nginx workers to their own unprivileged user; an *explicit* `USER root` remains a hard CI failure. | Set `PUID`/`PGID` in `.env` to match a real unprivileged user on your host if you want files written to the bind mounts to be owned by a specific host account rather than the default 1001:1001. |
| **Minimal capabilities** | Not set by the shipped `docker-compose.yml` — Docker's default capability set applies. | Add `cap_drop: ["ALL"]` plus only what this image's startup sequence actually uses — `CAP_CHOWN` (the entrypoint's `chown -R` of the bind mounts), `CAP_SETUID`/`CAP_SETGID` (supervisord dropping the backend to `appuser`; nginx's master process dropping workers to its own user), and `security_opt: ["no-new-privileges:true"]`. This is a starting point, not a verified minimal set for every host kernel/storage-driver combination — test it in staging first; a missing capability shows up immediately as the container failing to start or the PUID/PGID remap silently not applying, not as a subtle runtime failure. |
| **Private network** | The backend never listens on a published port — only nginx's 8080 is published (`docker-compose.yml:23-24`), and it's proxied to the backend over loopback inside the container. | Don't add other `ports:` entries. If you run other services in the same Docker Compose project (a reverse proxy, a monitoring stack), put them on a Docker network the mycorrhizal container doesn't need to reach, and only expose the reverse proxy's ports to the host. |
| **Restrictive filesystem permissions** | Inside the container, uploaded photo/attachment files are written with `0700`/`0750` permissions and served through authenticated controllers, never as static files (`docs/security/asvs-l2.md` V12.4.1, `photostore.go:137`, `attachments.go:40,48`). | On the host, the bind-mount directories (`DATA_PATH`/`PHOTOS_PATH`/`ATTACHMENTS_PATH`) hold every user's PII in the clear for the FTS-plaintext columns and all of it in the encrypted-at-rest columns' ciphertext (`threat-model.md` Gating decision 1) — restrict them to the `PUID`/`PGID` account, e.g. `chmod 700` the parent directory, so other accounts on the host can't read them. |
| **Resource limits** | Per-request/per-account throttles exist at the application layer (auth rate limiting, API rate limiting, CardDAV rate limiting, account lockout — see issue [#415](https://github.com/DrewBrunning/mycorrhizal-crm/issues/415)), but the container itself has no CPU/memory ceiling in the shipped `docker-compose.yml`. | Add `deploy.resources.limits` (Compose) or `mem_limit`/`cpus` to your override so a runaway process (or a bug the application-level limits don't catch) can't starve the rest of the host. Size it to your data volume — this is a single-process app, not one that needs elastic scaling. |
| **Encrypted, immutable backups** | A `VACUUM INTO` snapshot inherits the database's field-level AES-256-GCM at-rest encryption — encrypted columns travel as ciphertext, and the master key that unwraps them is never written into the snapshot (`docs/deployment.md` "Backup confidentiality & retention", issue [#420](https://github.com/DrewBrunning/mycorrhizal-crm/issues/420)). The app's backup-write surface is **write-new-only**: it refuses to overwrite, never rotates/expires/deletes an existing backup or pre-migration snapshot, and that is pinned by trying it (`backend/database/backup_immutability_test.go`, issue [#505](https://github.com/DrewBrunning/mycorrhizal-crm/issues/505)). | Three things stay entirely operator-owned: (1) the two plaintext directories (photos, attachments) and the FTS-plaintext columns are not covered by that encryption and need your own protection at rest (`age`/`gpg`/an encrypted volume) and in transit off-host (`rsync` over SSH, TLS to object storage); (2) retention/rotation of backup files — this app deliberately has no backup-expiry code (an app that can expire backups gives an attacker running as the app the same power, issue #505) — schedule your own pruning (keep it non-recursive so it never reaches the `pre-migration/` rollback points — example in `docs/deployment.md` "Retention & deletion"); (3) **immutability against a compromised host** — write-new-only is only a bug barrier on-host, since the app's own uid can still `rm` its files, so put backups where the app holds no credential to reach them: a pull-based off-host copy (recommended default) or object-locked remote storage, with the verify-by-trying procedure in `docs/deployment.md` "Backup immutability & ransomware resistance". |
| **Reverse proxy / firewall / DNS guidance** | Ships a working nginx example (`docs/deployment.md:19-30`) and documents the required env vars for a proxied deployment (`docs/deployment.md` "Production Environment"). | Own the reverse proxy config (TLS cipher policy — `docs/security/asvs-l2.md` marks strong-cipher selection `not-applicable` to the app for exactly this reason), the host firewall (only the reverse proxy's ports reachable from the internet; the mycorrhizal container's published port itself doesn't need to be internet-facing if the proxy runs on the same Docker network), and DNS (subdomain-takeover protection is operator-managed DNS hygiene, `docs/security/asvs-l2.md` V10.3.3). |
| **SSRF hardening (app-layer private-address block)** | Every outbound integration dials through `httputil.SafeDialContext` (`backend/httputil/safedial.go`). Five are guarded unconditionally — Immich, Paperless, Seafile, WebDAV/Nextcloud, and the update check. The rest — outbound webhooks, ntfy/Gotify/Web Push, CardDAV/CalDAV subscriptions, OIDC, the Monica importer — are guarded **only when the matching `*_BLOCK_PRIVATE_URLS` flag is set**. Those flags default to `false` (`backend/config/config.go`) so a trusted-LAN self-host can point webhooks and integrations at private addresses (issue [#373](https://github.com/DrewBrunning/mycorrhizal-crm/issues/373), the semgrep guardrail in [#609](https://github.com/DrewBrunning/mycorrhizal-crm/issues/609)). The engineering per-integration posture is `docs/int-01-integration-classification-matrix.md`. | For any instance reachable from the internet or hosting accounts you do not personally vet, set the whole opt-in set to `true`: `WEBHOOK_BLOCK_PRIVATE_URLS`, `CALDAV_BLOCK_PRIVATE_URLS`, `IMMICH_BLOCK_PRIVATE_URLS`, `PAPERLESS_BLOCK_PRIVATE_URLS`, `SEAFILE_BLOCK_PRIVATE_URLS`, `WEBDAV_BLOCK_PRIVATE_URLS`, `MONICA_BLOCK_PRIVATE_URLS`, `OIDC_BLOCK_PRIVATE_URLS` (`.env.example` "SSRF hardening"). With the flags off, an authenticated user who can set a webhook or integration URL can make the server reach loopback, other LAN hosts, and the cloud-metadata endpoint (`169.254.169.254`) — a network egress policy is a second layer, not a substitute for this one. Leave a single flag off only for an integration you genuinely reach on a private address. |

## What this application does not secure

Stated once, explicitly, so it stops being implied across a dozen tickets. If a threat lands in one
of these categories, no code change in this repository is the fix — it's an operator action against
the host/infrastructure:

- **The host OS.** Patching, kernel hardening, SSH exposure, local-user account hygiene — none of it
  is this app's concern or within its reach from inside a container.
- **The Docker daemon.** Its own attack surface (a Docker Engine CVE, a misconfigured daemon socket,
  rootless-vs-rootful mode) is infrastructure the app runs on top of, not something it patches.
- **The reverse proxy.** TLS cipher suite/protocol version selection, its own request logging, its
  own rate limiting or WAF rules, and its own software supply chain are the operator's proxy's
  concern (`docs/security/asvs-l2.md` V9.1.2 marks strong-cipher selection `not-applicable` for
  exactly this reason).
- **TLS certificates.** Issuance, renewal, revocation, and the private key material all live at the
  reverse proxy the operator chose; this app has no certificate of its own.
- **DNS.** Domain registration, record hygiene, and subdomain-takeover exposure are operator-managed
  (`docs/security/asvs-l2.md` V10.3.3).
- **The firewall / network perimeter.** Which ports are reachable from the internet, and from where,
  is a host/network decision this app cannot make for you — it only controls which ports it binds
  *inside* its own container.
- **The host filesystem, outside the three bind-mounted directories.** Host-level disk encryption,
  swap, and other processes' access to the same disk are outside the container boundary.
- **External backup storage.** Once a snapshot or `rsync` copy leaves the host, its confidentiality,
  durability, and lifecycle at rest (object storage bucket policy, off-site disk encryption, who else
  has read access to the destination) belong to whatever storage the operator chose. Backup
  *immutability* against a compromised host is in this category too: the application is write-new-only
  and holds no delete-a-backup capability, but its own uid can still destroy local files, so genuine
  immutability comes from the operator's off-host choice — a pull-based copy the app cannot reach, or
  object-locked storage with a write-only key (`docs/deployment.md` "Backup immutability & ransomware
  resistance", issue [#505](https://github.com/DrewBrunning/mycorrhizal-crm/issues/505)).
- **Host administrators.** Anyone with a legitimate host login can read the SQLite file, the mounted
  volumes, process memory, and environment variables directly — that is not a vulnerability in this
  app, it's what "the operator owns the host" means (`docs/security/threat-model.md` "The self-hosted
  boundary, as a first-class assumption").
- **Host compromise.** A live, logged-in root shell on the box can read every secret this app holds
  (`JWT_SECRET_KEY`, `DATA_ENCRYPTION_KEY`) and therefore decrypt everything the at-rest encryption
  protects. No software control defends against this, by definition, for a single-process self-hosted
  app — the at-rest encryption's actual threat model is a stolen disk or a stolen backup, not a live
  root shell (`docs/security/threat-model.md` Assets table, Gating decision 1).

Everything reachable over the network *without* host access — authentication, authorization, session
handling, input validation, SSRF protection — is not excused by any of the above and is covered like
any other application concern (`docs/security/asvs-l2.md` V2–V5, V9, V12, V13). One caveat: for the
integrations that take a user- or operator-supplied URL, the app-layer SSRF guard is **opt-in** and
defaults off for trusted-LAN self-hosting — an internet-exposed or multi-tenant deployment must turn
it on (the "SSRF hardening" row in the baseline table above).

## Verifying your deployment is on baseline

Commands an operator can run against their own instance, no source checkout required beyond the
compose file already in use:

```sh
# Response headers include HSTS (once COOKIE_SECURE=true) and the rest of the
# security header set, and never leak version/framework fingerprinting headers.
curl -sD - -o /dev/null https://your-domain.example.com/

# The backend process inside the container is not running as root.
docker compose exec mycorrhizal ps -o user= -C mycorrhizal

# Only the intended port is published to the host.
docker compose port mycorrhizal 8080

# The bind-mount directories are not world-readable on the host.
stat -c '%U:%G %a' ./data ./photos ./attachments

# No Docker socket is mounted into the container.
docker inspect mycorrhizal --format '{{json .Mounts}}' | grep -c docker.sock   # expect 0
```

## Keep this honest

- This doc, `docs/security/threat-model.md`'s self-hosted-boundary section, and
  `docs/security/asvs-l2.md`'s V1.2/V1.14 rows are one claim made in three places for three
  audiences (operator checklist, threat model, control-by-control audit) — a change to the deployment
  topology (a new published port, a new bind mount, a new sidecar container) updates all three in the
  same PR.
- A new external dependency (a second container, an external database, a message queue) needs a new
  row in the reference topology diagram and the baseline table above, not just a mention in
  `docs/deployment.md`.
