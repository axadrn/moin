# The hub image. Build from the repo root:
#   docker build --build-arg VERSION=v0.1.0 .
# The build runs on the builder's own CPU and cross-compiles for the
# target, so the arm64 box is built at full speed, not emulated.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /app

# Copy Go dependency files first so Docker can cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the project.
COPY . .

# Install build-time dependencies.
RUN apt-get update && apt-get install -y wget ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app/hub

# Generate Go files from .templ files.
RUN go tool templ generate

# The moin binary that install.sh, cli.sh, CI and daemon updates download
# from /download, built from the same commit as the hub, with
# SHA256SUMS for checking a download by hand. It is the same in both boxes,
# so it comes before anything of the target and is built once.
ARG VERSION=dev
RUN mkdir -p downloads && \
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
      os="${target%/*}" arch="${target#*/}" && \
      CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
        -ldflags "-s -w -X github.com/axadrn/moin/internal/build.Version=$VERSION" \
        -o "downloads/moin-$os-$arch" ../cmd/moin; \
    done && \
    (cd downloads && sha256sum moin-* > SHA256SUMS)

# The server, a static binary for the CPU of the box being built. It knows
# its version, the one daemons update to.
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags "-X github.com/axadrn/moin/internal/build.Version=$VERSION" \
      -o main ./cmd/server

# Deploy-Stage
FROM alpine:3.24
WORKDIR /app

# Install ca-certificates and sqlite for backups/debugging
RUN apk add --no-cache ca-certificates sqlite

# Set environment variable for runtime
ENV APP_ENV=production
ENV GO_ENV=production

# Copy the binary and downloads
COPY --from=build /app/hub/main .
COPY --from=build /app/hub/downloads ./downloads

# Expose the port
EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8090/healthz >/dev/null || exit 1

# Command to run
CMD ["./main"]
