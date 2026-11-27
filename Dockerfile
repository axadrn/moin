# The image of the site at moin.run. Build from the repo root:
#   docker build --build-arg VERSION=v0.1.0 .
# The build runs on the builder's own CPU and cross-compiles for the
# target, so the arm64 image is built at full speed, not emulated.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
WORKDIR /app

# Go dependency files first, so Docker caches the module download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

WORKDIR /app/cloud

# Go files from the .templ files.
RUN go tool templ generate

# The moin binaries of a release, built from the same commit, with
# SHA256SUMS for checking a download by hand. For a tag, the Deploy
# workflow takes them from the image, signs them and puts them on the
# GitHub release, where cli.sh and moin update download them. They are the same in every image, so they come before anything
# of the target and are built once.
ARG VERSION=dev
RUN mkdir -p downloads && \
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
      os="${target%/*}" arch="${target#*/}" && \
      CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
        -ldflags "-s -w -X github.com/axadrn/moin/internal/build.Version=$VERSION" \
        -o "downloads/moin-$os-$arch" ../cmd/moin; \
    done && \
    (cd downloads && sha256sum moin-* > SHA256SUMS)

# The site, a static binary for the CPU of the image being built.
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags "-X github.com/axadrn/moin/internal/build.Version=$VERSION" \
      -o site ./cmd/server

FROM alpine:3.24
WORKDIR /app
RUN apk add --no-cache ca-certificates
ENV APP_ENV=production
COPY --from=build /app/cloud/site .
COPY --from=build /app/cloud/downloads ./downloads
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --start-interval=2s --retries=3 CMD wget -qO- http://127.0.0.1:8090/healthz >/dev/null || exit 1
CMD ["./site"]
