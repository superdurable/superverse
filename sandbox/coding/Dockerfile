# syntax=docker/dockerfile:1.7

# The build must receive the Agent Sandbox v1.0.1 source tree as the named
# `agent_sandbox` context. This keeps the upstream source pin explicit without
# copying a mutable binary into this repository.
ARG GO_IMAGE
ARG NODE_IMAGE

FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS sandboxd-build
ARG GO_IMAGE
ARG NODE_IMAGE
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ENV GOTOOLCHAIN=local
WORKDIR /src

RUN validate_digest_ref() { \
      value="$1"; \
      case "${value}" in *@sha256:*) ;; *) return 1 ;; esac; \
      repository="${value%%@sha256:*}"; \
      digest="${value#*@sha256:}"; \
      test -n "${repository}"; \
      case "${repository}" in *@*) return 1 ;; esac; \
      test "${#digest}" -eq 64; \
      case "${digest}" in *[!0-9a-f]*) return 1 ;; esac; \
    }; \
    validate_digest_ref "${GO_IMAGE}"; \
    validate_digest_ref "${NODE_IMAGE}"; \
    test "${TARGETOS}" = linux; \
    case "${TARGETARCH}" in amd64|arm64) ;; *) exit 64 ;; esac; \
    test "${TARGETPLATFORM}" = "${TARGETOS}/${TARGETARCH}"

COPY --from=agent_sandbox go.mod go.sum ./
RUN --mount=type=cache,id=coding-sandbox-agent-sandbox-go-mod,target=/go/pkg/mod \
    go mod download
COPY --from=agent_sandbox packages/sandboxd/ ./packages/sandboxd/
COPY --from=agent_sandbox internal/ ./internal/
RUN mkdir -p /out
RUN --mount=type=cache,id=coding-sandbox-agent-sandbox-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=coding-sandbox-agent-sandbox-go-build-${TARGETOS}-${TARGETARCH},target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -buildvcs=false -trimpath -ldflags='-s -w' \
    -o /out/sandboxd ./packages/sandboxd/cmd/sandboxd

FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS app-dependencies
ENV GOTOOLCHAIN=local
WORKDIR /app-dependencies
# The image's own commands build in a module named like the repository, so the
# paths under sandbox/coding/ and its internal packages are those of the source tree.
COPY sandbox/coding/go.mod.imported ./go.mod
# Prewarm the application template's own module graphs; new projects start from them.
COPY app-template/go.mod app-template/go.sum ./template-go-dependencies/
COPY app-template/tools/openapi/go.mod app-template/tools/openapi/go.sum ./openapi-tools/
COPY sandbox/coding/cmd/ ./sandbox/coding/cmd/
COPY sandbox/coding/internal/ ./sandbox/coding/internal/
RUN --mount=type=cache,id=coding-sandbox-app-go-mod,target=/go/pkg/mod \
    cd openapi-tools \
    && go mod download all \
    && cd ../template-go-dependencies \
    && go mod download all
RUN --mount=type=cache,id=coding-sandbox-app-go-mod,target=/go/pkg/mod \
    cd openapi-tools \
    && go mod verify \
    && cd ../template-go-dependencies \
    && go mod verify
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,id=coding-sandbox-app-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=coding-sandbox-previewctl-go-build-${TARGETOS}-${TARGETARCH},target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -trimpath -ldflags='-s -w' -o /out/ \
      ./sandbox/coding/cmd/superverse-previewctl \
      ./sandbox/coding/cmd/superverse-exec \
      ./sandbox/coding/cmd/superverse-files
RUN --mount=type=cache,id=coding-sandbox-app-go-mod,target=/go/pkg/mod \
    mkdir -p /out/go-module-proxy \
    && cp -a /go/pkg/mod/cache/download/. /out/go-module-proxy/

FROM --platform=${TARGETPLATFORM} ${NODE_IMAGE} AS coding-base
ARG TARGETARCH
ARG DEXCLI_VERSION=1.6.4
ARG TEMPORAL_CLI_VERSION=1.8.1
ARG TEMPORAL_CLI_AMD64_SHA256=b94417b9a8760b30217f4b881dabce4b16a76a38b5e99e2eca3ce358b8030f06
ARG TEMPORAL_CLI_ARM64_SHA256=f622eba969aa58fdbe8a7b58feba7578b3fc935305079eb071d34bac12ffa5bb

LABEL io.superdurable.sandbox-image-contract-revision="8"

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      bash ca-certificates chromium-headless-shell coreutils curl git jq make procps python3 ripgrep tini \
    && rm -rf /var/lib/apt/lists/*

COPY --from=sandboxd-build /usr/local/go/ /usr/local/go/
COPY --from=sandboxd-build /out/sandboxd /usr/local/bin/sandboxd
COPY --from=app-dependencies /out/superverse-previewctl /out/superverse-exec /out/superverse-files /usr/local/bin/
COPY --from=app-dependencies /out/go-module-proxy/ /opt/superverse/go-module-proxy/
COPY dex-skills/ /opt/superverse/dex-skills/
COPY scripts/install-pinned-binary.sh /tmp/install-pinned-binary.sh
# The Agent's PATH holds guarded shims; previewctl runs the real binaries.
COPY --chmod=0755 sandbox/coding/shims/dexcli sandbox/coding/shims/temporal /usr/local/bin/

RUN test -f /opt/superverse/dex-skills/dex-app-builder/SKILL.md \
    && test -f /opt/superverse/dex-skills/dex-sdk/SKILL.md \
    && test -f /opt/superverse/dex-skills/dex-connector-contributor/SKILL.md \
    && test "${TEMPORAL_CLI_VERSION}" = 1.8.1 \
    && case "${TARGETARCH}" in \
         amd64) temporal_sha256="${TEMPORAL_CLI_AMD64_SHA256}" ;; \
         arm64) temporal_sha256="${TEMPORAL_CLI_ARM64_SHA256}" ;; \
         *) exit 64 ;; \
       esac \
    && temporal_archive="temporal_cli_${TEMPORAL_CLI_VERSION}_linux_${TARGETARCH}.tar.gz" \
    && curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
         "https://github.com/temporalio/cli/releases/download/v${TEMPORAL_CLI_VERSION}/${temporal_archive}" \
         --output "/tmp/${temporal_archive}" \
    && echo "${temporal_sha256}  /tmp/${temporal_archive}" | sha256sum --check --status \
    && mkdir -p /usr/local/libexec/superverse \
    && tar -xzf "/tmp/${temporal_archive}" -C /usr/local/libexec/superverse temporal \
    && rm "/tmp/${temporal_archive}" \
    && sh /tmp/install-pinned-binary.sh dexcli /usr/local/libexec/superverse/dexcli "v${DEXCLI_VERSION}" \
    && rm /tmp/install-pinned-binary.sh \
    && ln -s /usr/local/go/bin/go /usr/local/bin/go \
    && ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt \
    && bash -lc 'command -v go >/dev/null && command -v gofmt >/dev/null && go version >/dev/null' \
    && temporal --version | grep -F "1.8.1" >/dev/null \
    && dexcli version | grep -F "dexcli v${DEXCLI_VERSION} " >/dev/null \
    && test -x /usr/bin/tini \
    && git config --system --add safe.directory /workspace \
    && mkdir -p /workspace /var/lib/superverse/preview /tmp/go-build /tmp/npm-cache \
    && chown -R 1000:1000 /workspace /var/lib/superverse/preview /tmp/go-build /tmp/npm-cache

ENV HOME=/workspace \
    SUPERVERSE_SANDBOX_IMAGE_CONTRACT_REVISION=8 \
    PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin \
    CHROME_BIN=/usr/bin/chromium-headless-shell \
    PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/usr/bin/chromium-headless-shell \
    PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    XDG_CONFIG_HOME=/tmp/superverse-tool-execution/config \
    XDG_CACHE_HOME=/tmp/superverse-tool-execution/cache \
    GOPATH=/tmp/superverse-tool-execution/go \
    GOMODCACHE=/tmp/superverse-tool-execution/go/pkg/mod \
    GOPROXY=file:///opt/superverse/go-module-proxy,https://proxy.golang.org,direct \
    GOCACHE=/tmp/go-build \
    GOTELEMETRY=off \
    npm_config_cache=/tmp/npm-cache

WORKDIR /workspace
USER 1000:1000
EXPOSE 3000 8080 8801 9090
# tini is PID 1: it reaps the orphans of detached Preview processes and
# forwards termination to sandboxd. The coding profile's Pod command is the same.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/sandboxd"]
