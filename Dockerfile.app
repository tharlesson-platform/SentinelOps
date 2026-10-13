FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine3.23@sha256:9e45f0eb4a63ed37ad6e604950407558b7c738e34e015ae77a5d4ad369cde079 AS build
ARG APP
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
# Keep package pins compatible with the pinned Alpine package index in this base digest.
RUN apk add --no-cache ca-certificates=20260909-r0 git=2.52.0-r0
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
