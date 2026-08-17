FROM golang:1.26.6-alpine3.23 AS build
ARG APP
WORKDIR /src
RUN apk add --no-cache ca-certificates=20260611-r0 git=2.52.0-r0
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o /out/app ./apps/${APP}

FROM alpine:3.23.3
RUN apk add --no-cache \
        ca-certificates=20260611-r0 \
        libcrypto3=3.5.7-r0 \
        libssl3=3.5.7-r0 \
        musl=1.2.5-r23 \
        musl-utils=1.2.5-r23 \
        tzdata=2026c-r0 \
        zlib=1.3.2-r0 \
    && addgroup -S -g 10001 sentinel \
    && adduser -S -D -H -u 10001 -G sentinel sentinel \
    && mkdir -p /var/lib/sentinelops/artifacts \
    && chown -R sentinel:sentinel /var/lib/sentinelops
COPY --from=build --chown=10001:10001 /out/app /usr/local/bin/app
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/app"]
