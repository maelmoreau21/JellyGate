# =============================================================================
# JellyGate — Dockerfile (Multi-stage build)
# =============================================================================
# Ultra-lightweight multi-arch image (Alpine 3.23 + PostgreSQL 18 client tools)
# Ensures pg_dump/pg_restore major-match (PostgreSQL 18) while minimizing size.
# =============================================================================

# ── Step 1: Go binary compilation ───────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.26.6-alpine AS builder

# Arguments automatically injected by Docker Buildx for cross-compilation
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /build
RUN apk add --no-cache nodejs pnpm

# Copy dependency files first (optimized Docker cache)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Frontend dependencies to generate Tailwind locally
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc* tailwind.config.js ./
RUN PUPPETEER_SKIP_DOWNLOAD=true pnpm install --frozen-lockfile --config.minimum-release-age=0

# Copy the rest of the source code
COPY . .
RUN pnpm run build:css

# Compile the static binary (CGO disabled — SQLite via modernc.org/sqlite)
# TARGETOS and TARGETARCH are provided by Buildx during multi-arch build
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /build/jellygate \
      ./cmd/jellygate \
    && chmod 0550 /build/jellygate

# ── Step 2: Minimal final runtime image ─────────────────────────────────────
FROM alpine:3.23

# TLS certificates + timezone data + wget (healthcheck) + PostgreSQL 18 client utilities (pg_dump, pg_restore)
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    wget \
    postgresql18-client \
    && ln -sf /usr/bin/pg_dump /usr/local/bin/pg_dump \
    && ln -sf /usr/bin/pg_restore /usr/local/bin/pg_restore

# Non-root user for security (UID/GID 1000)
RUN addgroup -S -g 1000 jellygate && adduser -S -u 1000 -G jellygate jellygate

# Data directory
RUN mkdir -p /data && chown jellygate:jellygate /data

WORKDIR /app

# Copy the compiled binary with correct permissions in one layer (avoids duplicate layer size)
COPY --from=builder --chown=jellygate:jellygate /build/jellygate .

# Copy web assets (templates, static, locales)
COPY --from=builder --chown=jellygate:jellygate /build/web ./web

# Switch to non-root user
USER jellygate

# Volume for persistent data (SQLite, config)
VOLUME ["/data"]

# Default port
EXPOSE 8097

# Healthcheck
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=10s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8097/health || exit 1

# Entrypoint
ENTRYPOINT ["./jellygate"]
