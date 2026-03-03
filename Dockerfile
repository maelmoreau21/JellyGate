# =============================================================================
# JellyGate — Dockerfile (Multi-stage build)
# =============================================================================
# Image finale : ~10-15 Mo (Alpine + binaire Go statique, pure Go / sans CGO)
# =============================================================================

# ── Étape 1 : Compilation du binaire Go ─────────────────────────────────────
FROM golang:1.22-alpine AS builder

# Arguments injectés automatiquement par Docker Buildx pour le cross-compile
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /build

# Copier les fichiers de dépendances en premier (cache Docker optimisé)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copier le reste du code source
COPY . .

# Compiler le binaire statique (CGO désactivé — SQLite via modernc.org/sqlite)
# TARGETOS et TARGETARCH sont fournis par Buildx lors du multi-arch build
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS} \
    GOARCH=${TARGETARCH} \
    go build \
      -ldflags="-s -w" \
      -trimpath \
      -o /build/jellygate \
      ./cmd/jellygate

# ── Étape 2 : Image finale minimale ─────────────────────────────────────────
FROM alpine:3.19

# Certificats TLS (nécessaires pour LDAPS et SMTP TLS)
RUN apk add --no-cache ca-certificates tzdata wget

# Utilisateur non-root pour la sécurité
RUN addgroup -S jellygate && adduser -S jellygate -G jellygate

# Répertoire des données
RUN mkdir -p /data && chown jellygate:jellygate /data

WORKDIR /app

# Copier le binaire compilé
COPY --from=builder /build/jellygate .

# Copier les assets web (templates, static, locales)
COPY --from=builder /build/web ./web

# Passage en utilisateur non-root
USER jellygate

# Volume pour les données persistantes (SQLite, config)
VOLUME ["/data"]

# Port par défaut
EXPOSE 8097

# Healthcheck
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=10s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8097/ || exit 1

# Point d'entrée
ENTRYPOINT ["./jellygate"]
