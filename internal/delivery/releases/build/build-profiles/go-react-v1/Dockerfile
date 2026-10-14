# syntax=docker/dockerfile:1.7

# This is the product-owned build recipe for go-react-v1. Generated projects
# are not allowed to supply or override a Dockerfile.
ARG GO_BUILD_IMAGE
ARG NODE_BUILD_IMAGE
ARG STATIC_BASE_IMAGE

FROM ${NODE_BUILD_IMAGE} AS web-build
ARG GO_BUILD_IMAGE
ARG NODE_BUILD_IMAGE
ARG STATIC_BASE_IMAGE
WORKDIR /src

RUN validate_digest_ref() { \
      value="$1"; \
      case "${value}" in *@sha256:*) ;; *) return 1 ;; esac; \
      repository="${value%%@sha256:*}"; \
      digest="${value#*@sha256:}"; \
      test -n "${repository}"; \
      test "${#digest}" -eq 64; \
      case "${digest}" in *[!0-9a-f]*) return 1 ;; esac; \
    }; \
    validate_digest_ref "${GO_BUILD_IMAGE}"; \
    validate_digest_ref "${NODE_BUILD_IMAGE}"; \
    validate_digest_ref "${STATIC_BASE_IMAGE}"

COPY web/package.json web/package-lock.json ./web/
RUN --mount=type=cache,id=go-react-v1-npm,target=/root/.npm \
    npm --prefix web ci --ignore-scripts
COPY openapi/openapi.yaml ./openapi/openapi.yaml
COPY web/ ./web/
RUN --network=none \
    rm -rf web/src/api/generated && \
    npm --prefix web run generate && \
    npm --prefix web run build

FROM ${GO_BUILD_IMAGE} AS go-modules
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
WORKDIR /src
# Module downloads belong to immutable layers, not disposable cache mounts.
# Keep the tool and application manifests ahead of source for layer reuse.
COPY tools/openapi/go.mod tools/openapi/go.sum ./tools/openapi/
RUN go -C tools/openapi mod download
COPY go.mod go.sum* ./
RUN go mod download

FROM go-modules AS go-build
ENV GOPROXY=off GOSUMDB=off
COPY . ./
COPY --from=web-build /src/web/dist ./web/dist
RUN --network=none \
    --mount=type=cache,id=go-react-v1-go-build,target=/root/.cache/go-build \
    rm -rf internal/api/generated && \
    go -C tools/openapi tool ogen \
      --target ../../internal/api/generated \
      --package generated \
      ../../openapi/openapi.yaml && \
    go test ./... && \
    go vet ./... && \
    go build -buildvcs=false -trimpath -ldflags='-s -w' -o /out/app ./cmd/server

FROM ${STATIC_BASE_IMAGE}
WORKDIR /app
COPY --from=go-build --chown=65532:65532 /out/app /app/app
COPY --from=go-build --chown=65532:65532 /src/web/dist /app/web/dist
ENV PORT=8080 \
    DEX_BLOB_CACHE_DIR=/tmp/dex-blobs
USER 65532:65532
EXPOSE 8080 8811
ENTRYPOINT ["/app/app"]
