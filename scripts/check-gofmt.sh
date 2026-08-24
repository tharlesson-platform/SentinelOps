#!/bin/sh
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
docker run --rm -v "$ROOT:/src" -w /src golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 sh -ec \
  "find apps internal demo -name '*.go' -print0 | xargs -0 gofmt -d | tee /tmp/gofmt.diff; test ! -s /tmp/gofmt.diff"
