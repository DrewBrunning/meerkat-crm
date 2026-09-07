# Mycorrhizal CRM - All-in-one image
#
# Bundles React frontend and the Go backend into a single container, served by nginx
#
# Build context is the repository root:
#   docker build -t mycorrhizal-crm .

# =============================================================================
# Stage 1: Build Go backend (cross-compiled, CGO disabled - pure-Go SQLite)
# =============================================================================
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS backend-builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Copy go mod files first for better caching
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy backend source code
COPY backend/ .

# Version stamping. Passed as build args so the image records which source it
# was built from -- /health serves these, and they are what ties a user's bug
# report to a specific binary. Without them the binary reports "dev".
ARG APP_VERSION=dev
ARG APP_COMMIT=""
ARG APP_BUILD_DATE=""

# Build the application (glebarez/sqlite is pure Go (no CGO, i.e. without QEMU)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags "-X mycorrhizal/buildinfo.Version=${APP_VERSION} \
              -X mycorrhizal/buildinfo.Commit=${APP_COMMIT} \
              -X mycorrhizal/buildinfo.BuildDate=${APP_BUILD_DATE}" \
    -o mycorrhizal .

# =============================================================================
# Stage 2: Build React frontend (same-origin API, served by nginx)
# =============================================================================
# react-router 8 requires Node >=22.22.0 (engines field). Matches CI's own
# node-version: 22 pin (unit-tests.yml, e2e-tests.yml) rather than floating on
# node:lts-alpine the way frontend/Dockerfile (the split image) does -- this
# repo prefers an explicit, reproducible pin over a floating tag (see the Go
# toolchain note in CLAUDE.md). frontend/Dockerfile didn't need this change:
# lts-alpine already resolves past the floor (Node 24 as of 2026-08).
FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:2d984a15c9b54fd0aeb608b8e0d0d83529eb34d2966db27a1fb4f1edc3d298a3 AS frontend-builder

WORKDIR /app

# Node no longer bundles Corepack (and therefore the `yarn` shim) as of v26 --
# confirmed empirically: present on node:25-alpine, gone on node:26-alpine.
# Install yarn explicitly so a future Node bump doesn't silently break this
# build the way #297 (node:22-alpine -> node:26-alpine) did (exit 127, "yarn:
# not found").
#
# A plain `npm install -g yarn@<version>` is a semver pin, not a hash pin --
# OSSF Scorecard's Pinned-Dependencies check only credits `npm ci` or a
# git+https URL pinned to a 40-char commit SHA, and yarnpkg/yarn's git tags
# don't include the built lib/ directory (it's produced by `gulp build`,
# gitignored, only shipped in the published npm tarball), so a git-SHA
# install doesn't actually work. Download the exact release tarball instead
# and verify it against its published sha512 (cross-checked against the
# registry's `dist.integrity` for yarn@1.22.22) before installing.
RUN wget -q https://registry.npmjs.org/yarn/-/yarn-1.22.22.tgz -O /tmp/yarn.tgz && \
    printf '%s  /tmp/yarn.tgz\n' "a6b2f7906b721bba3d67d4aff083df04dad64c399707841b7acf00f6b133b7ac24255f2652fa22ae3534329dc6180534e98d17432037ff6fd140556e2bb3137e" > /tmp/yarn.sha512 && \
    sha512sum -c /tmp/yarn.sha512 && \
    npm install -g /tmp/yarn.tgz && \
    rm /tmp/yarn.tgz /tmp/yarn.sha512

# Copy package files first for better caching
COPY frontend/package.json frontend/yarn.lock ./

# Install dependencies. The repo is yarn-only (yarn.lock is committed, no
# package-lock.json), so both the install and the build below are
# unconditional yarn. The previous `if [ -f yarn.lock ] ... else npm ...`
# fallbacks were dead code (yarn.lock is always in context), and OSSF
# Scorecard flagged the `npm install` branch as an unpinned npm command (#331).
RUN yarn install --frozen-lockfile

# Copy frontend source code
COPY frontend/ .

# API is served from the same origin; nginx proxies /api to the backend.
# Must stay empty so the bundle uses relative URLs
ENV VITE_API_URL=""

# Stale-client identity (issue #475): stamp the web bundle with the same
# release this image is (APP_VERSION is passed to the build for the backend
# ldflags; ARG scope is per-stage so it is redeclared here), so a tab left
# open across a deploy can compare its own version against the server's
# /health contract. Defaults to "dev" like the backend buildinfo, which keeps
# an unstamped build inert in practice (dev vs dev never compares).
ARG APP_VERSION=dev
ARG VITE_APP_VERSION=${APP_VERSION}
ENV VITE_APP_VERSION=${VITE_APP_VERSION}

RUN yarn build

# =============================================================================
# Stage 3: Runtime - nginx + Go backend under supervisord
# =============================================================================
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

# Runtime dependencies. shadow provides usermod/groupmod for PUID/PGID remap.
# No sqlite package needed - the backend uses a pure-Go SQLite driver.
# Versions pinned (hadolint DL3018) against the alpine:3.24 index.
#
# The trailing `find ... chmod a-s` strips setuid/setgid bits (CIS 4.8,
# enforced by docker/cis-hardening.sh). `shadow` is installed only for
# usermod/groupmod (the PUID/PGID remap in docker/entrypoint.sh), but it drags
# in setuid-root helpers — passwd, chsh, chfn, chage, expiry, gpasswd, and
# setgid unix_chkpwd — that this image never calls. They are
# privilege-escalation surface for no benefit, so disarm them in place with the
# CIS-recommended remediation rather than hand-listing the seven names (which
# would silently miss any setuid binary a future apk bump adds).
#
# libssl3/libcrypto3 pinned explicitly, above what the base image ships
# (3.5.7-r0), to pull in the CVE-2026-14456 fix (3.5.8-r0) already present in
# this same pinned index -- caught by Trivy gating v0.6.1's release scan
# (exit-code: 1 on fixed CRITICAL/HIGH). No base-image digest bump needed;
# `apk policy libssl3` against this exact digest already lists 3.5.8-r0 from
# alpine/v3.24/main.
RUN apk add --no-cache \
    ca-certificates=20260611-r0 \
    tzdata=2026c-r0 \
    nginx=1.30.4-r1 \
    supervisor=4.3.0-r1 \
    shadow=4.18.0-r1 \
    libc6-compat=1.1.0-r4 \
    libssl3=3.5.8-r0 \
    libcrypto3=3.5.8-r0 \
    && find / -xdev -type f \( -perm -4000 -o -perm -2000 \) -exec chmod a-s {} +

WORKDIR /app

# Non-root user the backend process runs as
RUN addgroup -g 1001 -S appgroup && adduser -u 1001 -S appuser -G appgroup

# Runtime directories
RUN mkdir -p /app/data /app/static/photos /app/static/attachments /var/log/supervisor /run/nginx && \
    chown -R appuser:appgroup /app/data /app/static/photos /app/static/attachments

# Copy Go binary and static assets from the backend builder
COPY --from=backend-builder /app/mycorrhizal /app/mycorrhizal
COPY --from=backend-builder /app/static/styles.css /app/static/

# Copy frontend build to nginx html directory
COPY --from=frontend-builder /app/build /usr/share/nginx/html

# Configuration files
COPY docker/nginx.conf /etc/nginx/http.d/default.conf
COPY docker/supervisord.conf /etc/supervisor/conf.d/supervisord.conf
COPY docker/entrypoint.sh /app/entrypoint.sh

# docker/nginx.conf includes /etc/nginx/hsts.conf in every add_header block; it
# is (re)written by docker/entrypoint.sh at startup based on COOKIE_SECURE, but
# it must exist here too so the shipped nginx config is valid standalone.
RUN chmod +x /app/entrypoint.sh && : > /etc/nginx/hsts.conf

# Default environment
# PORT is the backend's internal bind port - nginx listens on 8080 (below) and
# proxies to it, so it must not collide with nginx's own port.
ENV PORT=8081
ENV SQLITE_DB_PATH=/app/data/mycorrhizal.db
ENV PROFILE_PHOTO_DIR=/app/static/photos
ENV ATTACHMENTS_DIR=/app/static/attachments
ENV GIN_MODE=release

# nginx listens on 8080 (no root needed to bind)
EXPOSE 8080

# Health check hits nginx, which proxies to the backend. It probes
# /health/live (liveness, issue #421) — is the process up — NOT /health (deep
# check) or /health/ready: a restart policy must not cycle the container just
# because an optional integration is unreachable or migrations are mid-run.
# JSON-array form (hadolint DL3025): wget --spider already exits non-zero on
# failure, so the shell form's `|| exit 1` was redundant.
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD ["wget", "--no-verbose", "--tries=1", "--spider", "http://127.0.0.1:8080/health/live"]

# Entrypoint remaps PUID/PGID + chowns data dirs, then launches supervisord
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
