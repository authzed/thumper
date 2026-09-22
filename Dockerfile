# syntax=docker/dockerfile:1

# Build stage runs natively on the builder's architecture ($BUILDPLATFORM) and
# cross-compiles to the requested target. thumper is a pure-Go, CGO-free static
# binary, so cross-compilation needs no emulation or C toolchain.
FROM --platform=$BUILDPLATFORM cgr.dev/chainguard/go:latest AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /work

# Download modules in their own layer so they're cached across source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -ldflags="-s -w" -o /out/thumper ./cmd/thumper/

# Minimal distroless runtime: no shell or package manager, runs as nonroot, and
# ships CA certificates and tzdata.
FROM cgr.dev/chainguard/static:latest
COPY --from=build /out/thumper /usr/bin/thumper

# Bundle the bundled scripts at the ko data location so that the released-image
# UX works here too: `thumper run scripts/example.yaml` resolves the file via
# KO_DATA_PATH (see internal/config/load.go findFile).
COPY scripts/ /var/run/ko/scripts/
ENV KO_DATA_PATH=/var/run/ko

ENTRYPOINT ["/usr/bin/thumper"]