# STAGE 1: Frontend Builder (Runs on host architecture for speed)
FROM --platform=$BUILDPLATFORM docker.io/hrfee/jfa-go-build-docker:latest AS frontend-builder
WORKDIR /opt/build
COPY . .
# Git is needed for version numbering in the Makefile/Go build
RUN git config --global --add safe.directory /opt/build
RUN npm ci
RUN CSSVERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "untagged") \
    env GOOS= GOARCH= CSSVERSION=$CSSVERSION make precompile > make_debug.log 2>&1 || (tail -n 100 make_debug.log && exit 1)

# STAGE 2: Go Builder (Runs on target architecture for native compilation)
FROM --platform=$TARGETPLATFORM docker.io/hrfee/jfa-go-build-docker:latest AS go-builder
ARG TARGETARCH
WORKDIR /opt/build

# Copy the ENTIRE build directory from stage 1 to include generated files (docs/docs.go, etc.)
COPY --from=frontend-builder /opt/build ./
RUN git config --global --add safe.directory /opt/build

# Build the Go binary natively
RUN set -e; \
    export VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/v//g' || echo "dev") && \
    export COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown") && \
    export BUILDTIME=$(date +%s) && \
    export CSSVERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "untagged") && \
    export LDFLAGS="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.cssVersion=${CSSVERSION} \
      -X main.buildTimeUnix=${BUILDTIME} \
      -X main.builtBy=docker \
      -X main.updater=docker" && \
    echo "Running go mod tidy..." && \
    go mod tidy && \
    echo "Starting native build for ${TARGETARCH}..." && \
    go build -v -tags "e2ee goolm external" -ldflags "${LDFLAGS}" -o /jellygate .

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
