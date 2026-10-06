# syntax=docker/dockerfile:1
# hne-server image (T047). The build stage runs on the build machine and cross-compiles for the
# target platform, so multi-arch builds need no emulation; the final stage only copies files.

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/hne-server ./cmd/hne-server
# Raw ARP needs CAP_NET_RAW. Granting it as a file capability lets the binary run as the
# distroless non-root user; Docker keeps NET_RAW in the container's capability set
# (compose.yaml also adds it explicitly).
RUN apt-get update && apt-get install -y --no-install-recommends libcap2-bin \
    && setcap cap_net_raw+ep /out/hne-server \
    && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hne-server /hne-server
COPY --from=build --chown=65532:65532 /out/data /data
ENV HNE_DATA=/data HNE_LISTEN=:8080
VOLUME /data
EXPOSE 8080
# distroless has no curl: the binary checks itself.
HEALTHCHECK --interval=10s --timeout=5s --start-period=10s --retries=3 CMD ["/hne-server", "healthcheck"]
ENTRYPOINT ["/hne-server"]
