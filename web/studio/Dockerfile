# syntax=docker/dockerfile:1.7
# Build from repository root: docker build -f web/studio/Dockerfile .
ARG NODE_IMAGE
FROM ${NODE_IMAGE} AS build
ARG NODE_IMAGE
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1
RUN value="${NODE_IMAGE}"; \
    case "${value}" in *@sha256:*) ;; *) exit 64 ;; esac; \
    digest="${value#*@sha256:}"; test "${#digest}" -eq 64; \
    case "${digest}" in *[!0-9a-f]*) exit 64 ;; esac
WORKDIR /app/web/studio
COPY web/studio/package.json web/studio/package-lock.json web/studio/.npmrc ./
RUN --mount=type=cache,id=sv2-studio-npm,target=/root/.npm npm ci
COPY web/studio/ ./
COPY api/superverse/v2/superverse.proto /app/api/superverse/v2/superverse.proto
RUN mkdir -p public && npm run build
FROM ${NODE_IMAGE}
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000 SUPERVERSE_PROTO_PATH=/app/api/superverse/v2/superverse.proto
COPY --from=build --chown=node:node /app/web/studio/.next/standalone ./
COPY --from=build --chown=node:node /app/web/studio/public ./web/studio/public
COPY --from=build --chown=node:node /app/web/studio/.next/static ./web/studio/.next/static
COPY --from=build --chown=node:node /app/api/superverse/v2/superverse.proto ./api/superverse/v2/superverse.proto
COPY --from=build --chown=node:node /app/web/studio/proto ./web/studio/proto
USER node
EXPOSE 3000
CMD ["node", "web/studio/server.js"]
