# Pin the toolchain minor version rather than tracking golang:alpine.
# go.mod declares go 1.25, but golang:alpine currently ships 1.26 — so release
# binaries were built by a compiler nobody chose, a minor version ahead of the
# one local `go test` uses. It also undermines the retag-vs-rebuild logic in
# container-publish.yml, whose premise is that identical source produces an
# equivalent image.
FROM golang:1.26-alpine AS builder

ARG VERSION=dev

RUN apk add --no-cache build-base

WORKDIR /src

# Copy module manifests first so `go mod download` is cacheable across
# code-only changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
        -ldflags "-X github.com/rwlove/PUMP/internal/web.Version=${VERSION}" \
        -o /pump ./cmd/pump

FROM scratch

ARG VERSION=dev
ARG REVISION=unknown

WORKDIR /app

COPY --from=builder /pump /app/

# OCI image-spec annotations (org.opencontainers.image.*). Baked in so any
# builder — local `podman build` or CI — produces a compliant image; CI's
# metadata-action overlays the dynamic created/revision/version at build time.
LABEL org.opencontainers.image.title="pump" \
      org.opencontainers.image.description="PUMP server (web UI + JSON API)" \
      org.opencontainers.image.source="https://github.com/rwlove/PUMP" \
      org.opencontainers.image.url="https://github.com/rwlove/PUMP" \
      org.opencontainers.image.documentation="https://github.com/rwlove/PUMP/blob/main/README.md" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

# Run as nobody. A static binary on scratch needs no user database, so this
# costs nothing, and it means the image is not root-by-default however it is
# run — not only under the HelmRelease that happens to set runAsUser.
USER 65534:65534

ENTRYPOINT ["./pump"]
