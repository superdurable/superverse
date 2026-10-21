# syntax=docker/dockerfile:1.7

# The build must receive the Agent Sandbox v1.0.1 source tree as the named
# `agent_sandbox` context. This keeps the upstream source pin explicit without
# copying a mutable binary into this repository.
ARG GO_IMAGE
ARG NODE_IMAGE
# The pinned Dex CLI, which coding-toolchain installs and coding-base checks
# through its shim.
ARG DEXCLI_VERSION=1.12.0

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

# Every example's composed tree, which make generate-app-template packs into the
# server's embedded archives: the files a Project created from a description or
# an example starts with, and the source of the module prewarm and the build
# cache seeds below. Only the examples the embedded catalog lists are unpacked,
# in its order: the first, the default, from app-template.tar, every other one
# from examples/<id>.tar, so an archive the catalog does not list is never
# seeded. /modules/<id> holds an example's module manifests and /seed/<id> the
# files the seeds compile and install; .ids in each lists the IDs, a name no ID
# can take, since an ID starts with a letter or a digit.
FROM --platform=${BUILDPLATFORM} ${NODE_IMAGE} AS template-source
COPY internal/dexai/app/apptemplate/app-template.tar /apptemplate/app-template.tar
COPY internal/dexai/app/apptemplate/examples/ /apptemplate/examples/
RUN set -eu; \
    node -e 'for (const {id} of JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))) { \
               if (!/^[a-z0-9][a-z0-9-]*$/.test(id)) throw new Error("unexpected example ID " + JSON.stringify(id)); \
               console.log(id); \
             }' /apptemplate/examples/catalog.json > /ids; \
    test -s /ids; \
    default="$(head -n 1 /ids)"; \
    while read -r id; do \
      archive="/apptemplate/examples/${id}.tar"; \
      if [ "${id}" = "${default}" ]; then archive=/apptemplate/app-template.tar; fi; \
      mkdir -p "/tree/${id}" "/modules/${id}/tools/openapi" "/seed/${id}/tools/openapi" "/seed/${id}/web"; \
      tar -xf "${archive}" -C "/tree/${id}"; \
      cd "/tree/${id}"; \
      cp go.mod go.sum "/modules/${id}/"; \
      cp tools/openapi/go.mod tools/openapi/go.sum "/modules/${id}/tools/openapi/"; \
      cp -R go.mod go.sum openapi cmd internal "/seed/${id}/"; \
      cp tools/openapi/go.mod tools/openapi/go.sum "/seed/${id}/tools/openapi/"; \
      cp web/package.json web/package-lock.json "/seed/${id}/web/"; \
    done < /ids; \
    cp /ids /modules/.ids; \
    cp /ids /seed/.ids

# The image's own commands build in a module named like the repository, so the
# paths under sandbox/coding/ and its internal packages are those of the source tree.
FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS image-commands
ENV GOTOOLCHAIN=local
WORKDIR /image-commands
COPY sandbox/coding/go.mod.imported ./go.mod
COPY sandbox/coding/cmd/ ./sandbox/coding/cmd/
COPY sandbox/coding/internal/ ./sandbox/coding/internal/
ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,id=coding-sandbox-app-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=coding-sandbox-previewctl-go-build-${TARGETOS}-${TARGETARCH},target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -trimpath -ldflags='-s -w' -o /out/ \
      ./sandbox/coding/cmd/superverse-previewctl \
      ./sandbox/coding/cmd/superverse-exec \
      ./sandbox/coding/cmd/superverse-files

# The image's module proxy: the module graphs of the application template and
# of each listed example (an example's go.mod may add connectors or select other
# releases than the base's), each with its ogen tool module. The downloads go
# through the shared module cache mount, so a module an earlier build fetched
# needs no network; the proxy is then filled from that mount into a module
# cache of this build's own, so it holds exactly these graphs and nothing that
# another build left in the mount.
FROM --platform=${BUILDPLATFORM} ${GO_IMAGE} AS app-dependencies
ENV GOTOOLCHAIN=local
WORKDIR /app-dependencies
COPY app-template/go.mod app-template/go.sum ./template-go-dependencies/
COPY app-template/tools/openapi/go.mod app-template/tools/openapi/go.sum ./template-go-dependencies/tools/openapi/
COPY --from=template-source /modules/ ./example-go-dependencies/
RUN --mount=type=cache,id=coding-sandbox-app-go-mod,target=/go/pkg/mod \
    set -eu; \
    graphs="template-go-dependencies template-go-dependencies/tools/openapi"; \
    while read -r id; do \
      graphs="${graphs} example-go-dependencies/${id} example-go-dependencies/${id}/tools/openapi"; \
    done < example-go-dependencies/.ids; \
    for graph in ${graphs}; do \
      go -C "${graph}" mod download all; \
      go -C "${graph}" mod verify; \
    done; \
    for graph in ${graphs}; do \
      GOMODCACHE=/tmp/proxy-module-cache GOPROXY=file:///go/pkg/mod/cache/download go -C "${graph}" mod download all; \
    done; \
    mkdir -p /out/go-module-proxy; \
    cp -a /tmp/proxy-module-cache/cache/download/. /out/go-module-proxy/; \
    GOMODCACHE=/tmp/proxy-module-cache go clean -modcache

# The toolchain the seeds are built with and the image runs: its packages, Go,
# Temporal and Dex CLI, the module proxy and the runtime environment. Besides
# the examples' own files, only a change to these rebuilds the seeds, and a
# change to the proxy comes after the Temporal and Dex CLI download. The image's
# own commands, the skills and the shims are added in coding-base, after the
# seeds.
FROM --platform=${TARGETPLATFORM} ${NODE_IMAGE} AS coding-toolchain
ARG TARGETARCH
ARG DEXCLI_VERSION
ARG TEMPORAL_CLI_VERSION=1.8.1
ARG TEMPORAL_CLI_AMD64_SHA256=b94417b9a8760b30217f4b881dabce4b16a76a38b5e99e2eca3ce358b8030f06
ARG TEMPORAL_CLI_ARM64_SHA256=f622eba969aa58fdbe8a7b58feba7578b3fc935305079eb071d34bac12ffa5bb

LABEL io.superdurable.sandbox-image-contract-revision="8"

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      bash ca-certificates chromium-headless-shell coreutils curl git jq make procps python3 ripgrep tini \
    && rm -rf /var/lib/apt/lists/*

COPY --from=sandboxd-build /usr/local/go/ /usr/local/go/
COPY scripts/install-pinned-binary.sh /tmp/install-pinned-binary.sh

RUN test "${TEMPORAL_CLI_VERSION}" = 1.8.1 \
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
    && /usr/local/libexec/superverse/temporal --version | grep -F "1.8.1" >/dev/null \
    && /usr/local/libexec/superverse/dexcli version | grep -F "dexcli v${DEXCLI_VERSION} " >/dev/null \
    && test -x /usr/bin/tini \
    && git config --system --add safe.directory /workspace \
    && mkdir -p /workspace /var/lib/superverse/preview /tmp/go-build /tmp/npm-cache \
    && chown -R 1000:1000 /workspace /var/lib/superverse/preview /tmp/go-build /tmp/npm-cache

COPY --from=app-dependencies /out/go-module-proxy/ /opt/superverse/go-module-proxy/

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

# The Go build cache and npm cache of every example, built by the image's own
# toolchain with its runtime environment and paths: Go keys a cache entry by the
# compiler, the package directory and its sources, so a project that starts
# from any example in /workspace finds its dependencies, the ogen tool and its
# unchanged packages already compiled. Each example in turn replaces
# /workspace and runs the commands of make generate (the Go half), make
# check-static and make test-app into the one cache, which stores a package
# that several examples share once. npm's cache is content-addressed, so each
# distinct lockfile installs once. This stage precedes coding-base, the other
# stage built from coding-toolchain: BuildKit (v0.33.1) appends both stages'
# history entries to the parent's shared list, and the stage it reads later is
# the one whose entries the image keeps.
FROM coding-toolchain AS app-build-cache
COPY --from=template-source /seed/ /tmp/superverse-examples/
RUN set -eu; \
    while read -r id; do \
      find /workspace -mindepth 1 -delete; \
      cp -R "/tmp/superverse-examples/${id}/." /workspace/; \
      go mod download; \
      go -C tools/openapi mod download; \
      go -C tools/openapi tool ogen --target ../../internal/api/generated --package generated ../../openapi/openapi.yaml; \
      go vet ./...; \
      go vet -tags apptest ./...; \
      go build ./...; \
      go test -tags apptest -count=1 -timeout 5m -run '^$' ./internal/apptest; \
    done < /tmp/superverse-examples/.ids; \
    rm -f "$(go env GOCACHE)/trim.txt"
RUN set -eu; \
    installed=; \
    while read -r id; do \
      lock="$(cat "/tmp/superverse-examples/${id}/web/package.json" "/tmp/superverse-examples/${id}/web/package-lock.json" | sha256sum | cut -d ' ' -f 1)"; \
      case " ${installed} " in *" ${lock} "*) continue ;; esac; \
      installed="${installed} ${lock}"; \
      rm -rf web; \
      mkdir web; \
      cp "/tmp/superverse-examples/${id}/web/package.json" "/tmp/superverse-examples/${id}/web/package-lock.json" web/; \
      npm --prefix web ci; \
    done < /tmp/superverse-examples/.ids; \
    rm -rf "${npm_config_cache}/_logs" "${npm_config_cache}/_update-notifier-last-checked"
# make bootstrap copies both seeds into the Pod's /tmp, an emptyDir whose 2 GiB
# sizeLimit kubelet compares with the blocks its files allocate, so the build
# fails when the seeds take more than 1 GiB of it. They are counted in 4 KiB
# blocks, as the node's filesystem stores them, whatever the build host's
# filesystem allocates: each file rounded up, each directory one block.
RUN set -eu; \
    seed_bytes="$(find "$(go env GOCACHE)" "${npm_config_cache}" -printf '%y %s\n' \
      | awk '{ blocks += ($1 == "d") ? 1 : int(($2 + 4095) / 4096) } END { printf "%.0f\n", blocks * 4096 }')"; \
    echo "the Go build cache and npm cache seeds take ${seed_bytes} bytes in 4 KiB blocks"; \
    if [ "${seed_bytes}" -gt 1073741824 ]; then \
      echo "the seeds take ${seed_bytes} bytes, above their 1 GiB budget in the Pod's /tmp (sandbox/coding/README.md#seed-budget)" >&2; \
      exit 1; \
    fi

# The coding image. The seeds are root-owned and read-only; the template's
# make bootstrap copies them into the Pod's own caches under /tmp. They come
# first, as the largest layers, which change only with the toolchain or an
# example; the image's own commands, the transport, the skills and the guarded
# shims on the Agent's PATH (previewctl runs the real binaries) follow, so a
# change to one of them rebuilds and republishes only the small layers after it.
FROM coding-toolchain AS coding-base
COPY --from=app-build-cache --chown=0:0 /tmp/go-build/ /opt/superverse/go-build-cache-seed/
COPY --from=app-build-cache --chown=0:0 /tmp/npm-cache/ /opt/superverse/npm-cache-seed/
ENV SUPERVERSE_GO_BUILD_CACHE_SEED=/opt/superverse/go-build-cache-seed \
    SUPERVERSE_NPM_CACHE_SEED=/opt/superverse/npm-cache-seed
ARG DEXCLI_VERSION
COPY --from=sandboxd-build /out/sandboxd /usr/local/bin/sandboxd
COPY --from=image-commands /out/superverse-previewctl /out/superverse-exec /out/superverse-files /usr/local/bin/
COPY dex-skills/ /opt/superverse/dex-skills/
COPY --chmod=0755 sandbox/coding/shims/dexcli sandbox/coding/shims/temporal /usr/local/bin/
# The shims run the real binaries with a throwaway HOME, so the check leaves
# nothing in the image's /workspace.
RUN test -f /opt/superverse/dex-skills/dex-app-builder/SKILL.md \
    && test -f /opt/superverse/dex-skills/dex-sdk/SKILL.md \
    && test -f /opt/superverse/dex-skills/dex-connector-contributor/SKILL.md \
    && check_home="$(mktemp -d)" \
    && HOME="${check_home}" temporal --version | grep -F "1.8.1" >/dev/null \
    && HOME="${check_home}" dexcli version | grep -F "dexcli v${DEXCLI_VERSION} " >/dev/null \
    && rm -rf "${check_home}"
EXPOSE 3000 8080 8801 9090
# tini is PID 1: it reaps the orphans of detached Preview processes and
# forwards termination to sandboxd. The coding profile's Pod command is the same.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/sandboxd"]
