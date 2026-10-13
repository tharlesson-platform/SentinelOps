#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
docker run --rm -v "$ROOT:/src" -w /src golang:1.26.9-alpine3.23@sha256:6750308561953d451210ed1a8086d36c8c8bdcbf672d38d9d0d424b8a7a0216c sh -ec \
  "find apps internal -name '*.go' -print0 | xargs -0 gofmt -d | tee /tmp/gofmt.diff; test ! -s /tmp/gofmt.diff"
