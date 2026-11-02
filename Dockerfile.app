FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS build
ARG APP
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
# The pinned official builder already includes Git and trust roots. Avoid
# fetching removed package revisions from a rolling Alpine package index.
RUN test -s /etc/ssl/certs/ca-certificates.crt && command -v git
COPY go.mod go.sum* ./
RUN go mod download
COPY apps ./apps
COPY internal ./internal
COPY dashboards ./dashboards
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -buildid=" -o /out/app ./apps/${APP}

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk upgrade --no-cache \
    && apk add --no-cache \
        ca-certificates \
        libcrypto3 \
        libssl3 \
        musl \
        musl-utils \
        tzdata \
        zlib \
    && addgroup -S -g 10001 sentinel \
    && adduser -S -D -H -u 10001 -G sentinel sentinel \
    && mkdir -p /var/lib/sentinelops/artifacts \
    && chown -R sentinel:sentinel /var/lib/sentinelops
COPY --from=build --chown=10001:10001 /out/app /usr/local/bin/app
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/app"]
