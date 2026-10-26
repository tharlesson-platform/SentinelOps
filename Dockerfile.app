FROM golang:1.27.1-alpine3.23@sha256:0908ac9b9319e09d7c238aabe914e0395c51d63c4e3d0ae8c554fda9158a5769 AS build
ARG APP
WORKDIR /src
# Keep package pins compatible with the pinned Alpine package index in this base digest.
RUN apk add --no-cache ca-certificates=20260909-r0 git=2.52.0-r0
COPY go.mod go.sum* ./
RUN go mod download
COPY apps ./apps
COPY internal ./internal
COPY dashboards ./dashboards
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o /out/app ./apps/${APP}

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
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
