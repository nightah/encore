# syntax=docker/dockerfile:1
#
# Local and ad-hoc images:
#   docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -t encore .
# Release images are built by GoReleaser from prebuilt binaries (see Dockerfile.goreleaser).
#
# The runtime is antilax3's Alpine s6-overlay base: PUID/PGID select the user, /config holds Encore's data, and docker/root adds the Encore services and banner.

ARG BASE_IMAGE="antilax3/alpine:latest"
ARG NODE_VERSION=24
ARG GO_VERSION=1.27
ARG PNPM_VERSION=12.12.1

# ---- web: build the SPA once, on the build host's architecture ----
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-bookworm-slim AS web
ARG PNPM_VERSION
# corepack is not shipped in the Node 24+ images; install pnpm explicitly.
RUN npm install -g pnpm@${PNPM_VERSION}
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
# vite writes the bundle to ../internal/server/dist
RUN mkdir -p /src/internal/server/dist && pnpm build

# ---- go: cross-compile for the target platform from the build host ----
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS go
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG version=dev
ARG commit=""
ARG build_date=""
WORKDIR /src
# No separate `go mod download`: it would also fetch tool dependencies (air) that the build never uses; go build fetches only what Encore needs into the same cache.
COPY . .
COPY --from=web /src/internal/server/dist/ ./internal/server/dist/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; \
    GOARM=""; \
    if [ "${TARGETARCH}" = "arm" ]; then GOARM="${TARGETVARIANT#v}"; fi; \
    pkg=github.com/nightah/encore/internal/buildinfo; \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" GOARM="${GOARM}" \
      go build -trimpath \
        -ldflags "-s -w -X ${pkg}.Version=${version} -X ${pkg}.Commit=${commit} -X ${pkg}.Date=${build_date}" \
        -o /out/encore ./cmd/encore

# ---- runtime ----
FROM ${BASE_IMAGE}

# set version labels
ARG build_date
ARG version
LABEL build_date="${build_date}"
LABEL version="${version}"
LABEL maintainer="Nightah"
LABEL org.opencontainers.image.title="encore" \
      org.opencontainers.image.description="Self-hosted Alexa skill server for Kodi" \
      org.opencontainers.image.url="https://github.com/nightah/encore" \
      org.opencontainers.image.source="https://github.com/nightah/encore" \
      org.opencontainers.image.licenses="Apache-2.0"

ENV ENCORE_DATA_DIR="/config"

# copy local files
COPY --link docker/root/ /
COPY --link --from=go /out/encore /app/encore

# ports, volumes and healthcheck
EXPOSE 8080
VOLUME /config
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/app/encore", "healthcheck"]
