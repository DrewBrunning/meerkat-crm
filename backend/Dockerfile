# Build stage
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Version stamping, same as the all-in-one image at /Dockerfile. This is a
# separately published image, so it needs its own stamp -- without it /health
# reports "dev" and a bug report cannot be tied to a build.
ARG APP_VERSION=dev
ARG APP_COMMIT=""
ARG APP_BUILD_DATE=""

# Build the application. -trimpath drops the builder's absolute paths and
# -buildvcs=false keeps VCS state out, so the binary is byte-identical across
# machines -- the commit comes from the -X ldflag (REL-04, issue #448;
# docs/security/reproducible-builds.md).
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath -buildvcs=false \
    -ldflags "-X mycorrhizal/buildinfo.Version=${APP_VERSION} \
              -X mycorrhizal/buildinfo.Commit=${APP_COMMIT} \
              -X mycorrhizal/buildinfo.BuildDate=${APP_BUILD_DATE}" \
    -o mycorrhizal .

# Runtime stage
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

# Install runtime dependencies (versions pinned per hadolint DL3018 against
# the alpine:3.24 index).
#
# libssl3/libcrypto3 pinned explicitly, above what the base image ships
# (3.5.7-r0), to pull in the CVE-2026-14456 fix (3.5.8-r0) already present in
# this same pinned index -- caught by Trivy gating v0.6.1's release scan
# (exit-code: 1 on fixed CRITICAL/HIGH). No base-image digest bump needed;
# `apk policy libssl3` against this exact digest already lists 3.5.8-r0 from
# alpine/v3.24/main.
RUN apk add --no-cache ca-certificates=20260611-r0 tzdata=2026c-r0 shadow=4.18.0-r1 su-exec=0.3-r0 libc6-compat=1.1.0-r4 libssl3=3.5.8-r0 libcrypto3=3.5.8-r0

WORKDIR /app

# Create non-root user for security
RUN addgroup -g 1001 -S appgroup && adduser -u 1001 -S appuser -G appgroup

# Create directories for data persistence
RUN mkdir -p /app/static/photos /app/data && \
    chown -R appuser:appgroup /app

# Set default environment
ENV PORT=8080
ENV SQLITE_DB_PATH=/app/data/mycorrhizal.db
ENV PROFILE_PHOTO_DIR=/app/static/photos

# Copy the binary from builder
COPY --from=builder /app/mycorrhizal .

# Copy static assets if any
COPY --from=builder /app/static/styles.css ./static/

# Copy entrypoint script
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Expose the port
EXPOSE 8080

# Issue #578: the split backend image is a published artifact (docker-publish
# matrix) and must carry a HEALTHCHECK like the all-in-one does — trivy's
# DS-0026 flagged its absence once the misconfig gate hard-gated. The Go
# server serves the health surface directly on 8080 (backend/routes/routes.go),
# no auth. Probe /health/live (liveness, issue #421): a restart policy wants
# "is the process up", not the deep check or readiness.
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD ["wget", "--no-verbose", "--tries=1", "--spider", "http://127.0.0.1:8080/health/live"]

# Run as root initially; entrypoint drops to appuser after adjusting UID/GID
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["./mycorrhizal"]
