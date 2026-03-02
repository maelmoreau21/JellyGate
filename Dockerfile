FROM --platform=$BUILDPLATFORM docker.io/hrfee/jfa-go-build-docker:latest AS builder
ARG BUILT_BY
ARG TARGETARCH

ENV JFA_GO_BUILT_BY=${BUILT_BY:-"docker"}

WORKDIR /opt/build
COPY . .

# Install frontend dependencies
RUN npm ci

# Compute version info and build all frontend assets (CSS, TypeScript, HTML, email, swagger, config)
RUN CSSVERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "untagged") \
    env GOOS= GOARCH= CSSVERSION=$CSSVERSION make precompile

# Cross-compile the Go binary for the target architecture
RUN set -e; \
    VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/v//g' || echo "dev"); \
    COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown"); \
    BUILDTIME=$(date +%s); \
    CSSVERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "untagged"); \
    LDFLAGS="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.cssVersion=${CSSVERSION} \
      -X main.buildTimeUnix=${BUILDTIME} \
      -X main.builtBy=${JFA_GO_BUILT_BY} \
      -X main.updater=docker"; \
    case "${TARGETARCH}" in \
      amd64) \
        CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
        CC=x86_64-linux-gnu-gcc \
        CXX=x86_64-linux-gnu-g++ \
        PKG_CONFIG_PATH=/usr/lib/x86_64-linux-gnu/pkgconfig \
        go build -tags "e2ee goolm external" -ldflags "${LDFLAGS}" -o dist/jfa-go . ;; \
      arm64) \
        CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
        CC=aarch64-linux-gnu-gcc \
        CXX=aarch64-linux-gnu-g++ \
        PKG_CONFIG_PATH=/usr/lib/aarch64-linux-gnu/pkgconfig \
        go build -tags "e2ee goolm external" -ldflags "${LDFLAGS}" -o dist/jfa-go . ;; \
      arm) \
        CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 \
        CC=arm-linux-gnueabihf-gcc \
        CXX=arm-linux-gnueabihf-g++ \
        PKG_CONFIG_PATH=/usr/lib/arm-linux-gnueabihf/pkgconfig \
        go build -tags "e2ee goolm external" -ldflags "${LDFLAGS}" -o dist/jfa-go . ;; \
      *) echo "Unsupported TARGETARCH: ${TARGETARCH}" && exit 1 ;; \
    esac

RUN sed -i 's#id="password_resets-watch_directory" placeholder="/config/jellyfin"#id="password_resets-watch_directory" value="/jf" disabled#g' build/data/html/setup.html

FROM gcr.io/distroless/base:latest AS final
COPY --from=builder /opt/build/dist/jfa-go /jfa-go/jfa-go
COPY --from=builder /opt/build/build/data /jfa-go/data

EXPOSE 8056
EXPOSE 8057

CMD [ "/jfa-go/jfa-go", "-data", "/data" ]
