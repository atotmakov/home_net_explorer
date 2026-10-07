# syntax=docker/dockerfile:1
# hne-server image (T047). The build stage runs on the build machine and cross-compiles for the
# target platform, so multi-arch builds need no emulation; the final stage only copies files.

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
# VERSION and COMMIT are stamped by CI (T099); a plain local build reports dev/unknown.
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION -X main.commit=$(echo $COMMIT | cut -c1-7)" \
    -o /out/hne-server ./cmd/hne-server
# Raw ARP needs CAP_NET_RAW. Granting it as a file capability lets the binary run as the
# distroless non-root user; Docker keeps NET_RAW in the container's capability set
# (compose.yaml also adds it explicitly).
RUN apt-get update && apt-get install -y --no-install-recommends libcap2-bin \
    && setcap cap_net_raw+ep /out/hne-server \
    && mkdir -p /out/data

# Collector binaries served at /downloads/ (T067). This stage doesn't depend on the target
# platform, so a multi-arch build compiles them once.
FROM --platform=$BUILDPLATFORM golang:1.26 AS collectors
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN for t in windows/amd64 linux/amd64 linux/arm64; do \
        os=${t%/*}; arch=${t#*/}; ext=""; \
        if [ "$os" = windows ]; then ext=.exe; fi; \
        CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
            -ldflags "-s -w -X main.version=$VERSION -X main.commit=$(echo $COMMIT | cut -c1-7)" \
            -o /out/downloads/hne-collector-$os-$arch$ext ./cmd/hne-collector || exit 1; \
    done

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hne-server /hne-server
COPY --from=collectors /out/downloads /app/downloads
COPY --from=build --chown=65532:65532 /out/data /data
ENV HNE_DATA=/data HNE_LISTEN=:8080 HNE_DOWNLOADS=/app/downloads
VOLUME /data
EXPOSE 8080
# distroless has no curl: the binary checks itself.
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 CMD ["/hne-server", "healthcheck"]
ENTRYPOINT ["/hne-server"]
