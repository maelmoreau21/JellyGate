# =============================================================================
# JellyGate — Dockerfile (Multi-stage build)
# =============================================================================
# Postgres 18 runtime base to ensure pg_dump/pg_restore major-match in Docker.
# =============================================================================

# ── Step 1: Go binary compilation ───────────────────────────────────────────
FROM golang:1.26.6-alpine AS builder

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
      ./cmd/jellygate

# ── Step 2: Minimal final image ─────────────────────────────────────────────
FROM postgres:18-alpine

# TLS certificates + utility tools + Postgres server cleanup to minimize image size
RUN apk add --no-cache ca-certificates tzdata wget \
    && rm -rf /usr/local/bin/postgres \
              /usr/local/bin/initdb \
              /usr/local/bin/pg_ctl \
              /usr/local/bin/pg_controldata \
              /usr/local/bin/pg_resetwal \
              /usr/local/bin/pg_receivewal \
              /usr/local/bin/pg_recvlogical \
              /usr/local/bin/pg_waldump \
              /usr/local/lib/postgresql \
              /usr/local/share/postgresql \
              /var/lib/postgresql

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
  CMD wget --no-verbose --tries=1 --spider http://localhost:8097/health || exit 1

# Entrypoint
ENTRYPOINT ["./jellygate"]
