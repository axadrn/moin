# The control plane image. Build from the repo root:
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

# Download the pinned Tailwind standalone binary for the current CPU architecture.
RUN arch="$(dpkg --print-architecture)" && \
    case "$arch" in \
      amd64) tailwind_arch="x64"; tailwind_sha256="dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a" ;; \
      arm64) tailwind_arch="arm64"; tailwind_sha256="55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195" ;; \
      *) echo "unsupported architecture: $arch" && exit 1 ;; \
    esac && \
    wget -qO /usr/local/bin/tailwindcss "https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-${tailwind_arch}" && \
    echo "${tailwind_sha256}  /usr/local/bin/tailwindcss" | sha256sum --check - && \
    chmod +x /usr/local/bin/tailwindcss

WORKDIR /app/controlplane

# Compile Tailwind CSS.
RUN tailwindcss -i ./assets/css/globals.css -o ./assets/css/output.css --minify

# Generate Go files from .templ files.
RUN go tool templ generate

# The moin binary that install.sh, cli.sh, CI and daemon updates download
# from /download, built from the same commit as the control plane, with
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

# Copy the binary, CSS output, and content directory
COPY --from=build /app/controlplane/main .
COPY --from=build /app/controlplane/assets/css/output.css ./assets/css/output.css
COPY --from=build /app/controlplane/content ./content
COPY --from=build /app/controlplane/downloads ./downloads

# Expose the port
EXPOSE 8090

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8090/healthz >/dev/null || exit 1

# Command to run
CMD ["./main"]
