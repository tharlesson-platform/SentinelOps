#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
docker run --rm -v "$ROOT:/src" -w /src golang:1.26.10-alpine3.23@sha256:34414731924cf2263160f0021f5cf6e1807382d85248f7cb419d4187b96d5e43 sh -ec \
  "find apps internal -name '*.go' -print0 | xargs -0 gofmt -d | tee /tmp/gofmt.diff; test ! -s /tmp/gofmt.diff"
