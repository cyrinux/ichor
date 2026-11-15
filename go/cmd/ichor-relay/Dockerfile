# ichor-relay image: `docker build -f go/cmd/ichor-relay/Dockerfile go/` (the context is the Go
# module), published by .github/workflows/release.yml as ghcr.io/cyrinux/ichor-relay:<tag>.
# A static binary on distroless: no shell, no package manager, runs as nonroot (65532), so the
# pod passes the restricted Pod Security profile (deploy/relay).

# The builder runs on the build host's platform and cross-compiles: no emulation for arm64.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/ichor-relay ./cmd/ichor-relay

FROM gcr.io/distroless/static:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="ichor-relay" \
      org.opencontainers.image.description="Receives Alertmanager's webhook in the cluster and pushes the alerts to the phones paired from Ichor" \
      org.opencontainers.image.source="https://github.com/cyrinux/ichor" \
      org.opencontainers.image.url="https://github.com/cyrinux/ichor/blob/main/docs/relay.md" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/ichor-relay /ichor-relay
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/ichor-relay"]
