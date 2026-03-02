# STAGE 1: Frontend Builder (Runs on host architecture for speed)
FROM --platform=$BUILDPLATFORM docker.io/hrfee/jfa-go-build-docker:latest AS frontend-builder
WORKDIR /opt/build
COPY . .
RUN npm ci
RUN CSSVERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "untagged") \
    env GOOS= GOARCH= CSSVERSION=$CSSVERSION make precompile

# STAGE 2: Go Builder (Runs on target architecture to ensure native CGO compatibility)
FROM golang:1.24-bookworm AS go-builder
ARG TARGETARCH
WORKDIR /opt/build

# Install native dependencies required for CGO (libolm, sqlite, etc.)
RUN apt-get update && apt-get install -y \
    libolm-dev \
    libsqlite3-dev \
    git \
    && rm -rf /var/lib/apt/lists/*

# Copy source and precompiled frontend assets
COPY . .
COPY --from=frontend-builder /opt/build/build/data ./build/data

# Build the Go binary natively
RUN VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/v//g' || echo "dev") && \
    COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown") && \
    BUILDTIME=$(date +%s) && \
    CSSVERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "untagged") && \
    LDFLAGS="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.cssVersion=${CSSVERSION} \
      -X main.buildTimeUnix=${BUILDTIME} \
      -X main.builtBy=docker \
      -X main.updater=docker" && \
    go build -tags "e2ee goolm external" -ldflags "${LDFLAGS}" -o /jellygate .

# Final cleanup of the data folder (specific project logic)
RUN sed -i 's#id="password_resets-watch_directory" placeholder="/config/jellyfin"#id="password_resets-watch_directory" value="/jf" disabled#g' build/data/html/setup.html

# STAGE 3: Final Image
FROM gcr.io/distroless/base:latest AS final
WORKDIR /jfa-go
COPY --from=go-builder /jellygate /jfa-go/jfa-go
COPY --from=go-builder /opt/build/build/data /jfa-go/data

EXPOSE 8056
EXPOSE 8057

CMD [ "/jfa-go/jfa-go", "-data", "/data" ]
