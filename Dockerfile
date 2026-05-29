# =============================================================================
# JellyGate — Dockerfile (Multi-stage build)
# =============================================================================
# Postgres 18 runtime base to ensure pg_dump/pg_restore major-match in Docker.
# =============================================================================

# ── Step 1: Go binary compilation ───────────────────────────────────────────
FROM golang:1.26.3-alpine AS builder

# Arguments automatically injected by Docker Buildx for cross-compilation
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /build
RUN apk add --no-cache nodejs npm

# Copy dependency files first (optimized Docker cache)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Frontend dependencies to generate Tailwind locally
COPY package.json package-lock.json tailwind.config.js ./
RUN npm ci

# Copy the rest of the source code
COPY . .
RUN npm run build:css

# Compile the static binary (CGO disabled — SQLite via modernc.org/sqlite)
# TARGETOS and TARGETARCH are provided by Buildx during multi-arch build
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /build/jellygate \
      ./cmd/jellygate

# ── Step 2: Minimal final image ─────────────────────────────────────────────
FROM postgres:18-alpine

# TLS certificates + utility tools
RUN apk add --no-cache ca-certificates tzdata wget

# Non-root user for security
RUN addgroup -S jellygate && adduser -S jellygate -G jellygate

# Data directory
RUN mkdir -p /data && chown jellygate:jellygate /data

WORKDIR /app

# Copy the compiled binary
COPY --from=builder --chown=jellygate:jellygate /build/jellygate .

# Copy the web assets (templates, static, locales)
COPY --from=builder --chown=jellygate:jellygate /build/web ./web

RUN chmod 0550 /app/jellygate

# Switch to non-root user
USER jellygate

# Volume for persistent data (SQLite, config)
VOLUME ["/data"]

# Default port
EXPOSE 8097

# Healthcheck
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=10s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8097/ || exit 1

# Entrypoint
ENTRYPOINT ["./jellygate"]
