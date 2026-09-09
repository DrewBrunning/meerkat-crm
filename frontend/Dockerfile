# Build stage
FROM --platform=$BUILDPLATFORM node:26-alpine@sha256:2d984a15c9b54fd0aeb608b8e0d0d83529eb34d2966db27a1fb4f1edc3d298a3 AS builder

WORKDIR /app

# node:26-alpine no longer bundles Corepack/Yarn (Corepack was dropped from the
# default Node.js distribution), so the classic Yarn CLI has to be installed
# explicitly to honor yarn.lock (lockfile v1).
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
COPY package.json yarn.lock ./

# Install dependencies. The repo is yarn-only (yarn.lock is committed, no
# package-lock.json), so both the install and the build below are
# unconditional yarn. The previous `if [ -f yarn.lock ] ... else npm ...`
# fallbacks were dead code (yarn.lock is always in context), and OSSF
# Scorecard flagged the `npm install` branch as an unpinned npm command (#331).
RUN yarn install --frozen-lockfile

# Copy source code
COPY . .

# Build the app with the API URL injected at build time
ARG VITE_API_URL
ENV VITE_API_URL=${VITE_API_URL}

# Stale-client identity (issue #475): the web bundle is stamped with the
# release it was built from so a tab left open across a deploy can compare its
# own version against the server's /health contract and force a reload when it
# falls below the declared floor. docker-publish.yml passes APP_VERSION to
# every build context (the same arg the backend ldflags consume); it defaults
# to "dev" exactly like the backend buildinfo, which keeps an unstamped
# mechanism inert in practice (dev vs dev never compares). Unset leaves the
# mechanism disabled.
ARG APP_VERSION=dev
ARG VITE_APP_VERSION=${APP_VERSION}
ENV VITE_APP_VERSION=${VITE_APP_VERSION}

RUN yarn build

# Production stage - serve with unprivileged nginx
FROM nginxinc/nginx-unprivileged:1.31-alpine@sha256:2ddec616f1cb58bcac057aa388f28cb81e35137641ef4226d321714499329bd1

# The base image's own alpine 3.24.1 layer ships libssl3/libcrypto3 3.5.7-r0,
# which CVE-2026-14456 fixes in 3.5.8-r0 -- already present in this same
# pinned index (`apk policy libssl3` against this digest lists it from
# alpine/v3.24/main), so no base-image digest bump is needed. Caught by Trivy
# gating v0.6.1's release scan (exit-code: 1 on fixed CRITICAL/HIGH).
#
# `apk add` needs root; the base image's own final layer sets USER 101
# (nginx), which every RUN here would otherwise inherit. Switch to root only
# for the upgrade and back to 101 immediately after -- CMD must not run as
# root, which is the entire point of the "-unprivileged" base image. Numeric
# UIDs (not `root`/`nginx`) per hadolint DL3066 -- a named user may not
# resolve the same way on every host.
USER 0
RUN apk add --no-cache libssl3=3.5.8-r0 libcrypto3=3.5.8-r0
USER 101

# Copy custom nginx config
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy built assets from builder
COPY --from=builder /app/build /usr/share/nginx/html

# nginx-unprivileged runs on port 8080 by default
EXPOSE 8080

# Issue #578: the split frontend image is a published artifact (docker-publish
# matrix) and must carry a HEALTHCHECK like the all-in-one does — trivy's
# DS-0026 flagged its absence once the misconfig gate hard-gated. Probes the
# SPA root (/) rather than /health, which proxies to the backend host and
# would 502 when the frontend runs standalone.
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD ["wget", "--no-verbose", "--tries=1", "--spider", "http://127.0.0.1:8080/"]

# Start nginx
CMD ["nginx", "-g", "daemon off;"]
