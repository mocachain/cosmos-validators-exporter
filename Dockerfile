# syntax=docker/dockerfile:1.7
# Multi-stage build for cosmos-validators-exporter.
# Produces a small static binary on a distroless base, multi-arch ready.

ARG GO_VERSION=1.25.8

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# Pull deps first for better layer cache
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# Static, stripped, reproducible-ish binary.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w -extldflags "-static"' \
      -o /out/cosmos-validators-exporter ./cmd/cosmos-validators-exporter.go

# ────────────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/cosmos-validators-exporter /usr/local/bin/cosmos-validators-exporter

USER nonroot:nonroot
EXPOSE 9560
ENTRYPOINT ["/usr/local/bin/cosmos-validators-exporter"]
CMD ["--config=/config/config.toml"]
