FROM --platform=$BUILDPLATFORM docker.io/hrfee/jfa-go-build-docker:latest AS support
ARG BUILT_BY
ENV JFA_GO_BUILT_BY=$BUILT_BY

WORKDIR /opt/build
COPY . .

# Install frontend dependencies
RUN npm ci

# Build all frontend assets (CSS, TypeScript, HTML, email templates, swagger docs, config)
RUN env GOOS= GOARCH= make precompile

# Build the Go binary using goreleaser (skipping before hooks, already done above)
RUN bash ./scripts/version.sh goreleaser build --snapshot --skip=validate,before --clean --id notray-e2ee

RUN mv /opt/build/dist/*_linux_arm_6 /opt/build/dist/placeholder_linux_arm
RUN sed -i 's#id="password_resets-watch_directory" placeholder="/config/jellyfin"#id="password_resets-watch_directory" value="/jf" disabled#g' /opt/build/build/data/html/setup.html

FROM gcr.io/distroless/base:latest AS final
ARG TARGETARCH

COPY --from=support /opt/build/dist/*_linux_${TARGETARCH}* /jfa-go
COPY --from=support /opt/build/build/data /jfa-go/data

EXPOSE 8056
EXPOSE 8057

CMD [ "/jfa-go/jfa-go", "-data", "/data" ]
