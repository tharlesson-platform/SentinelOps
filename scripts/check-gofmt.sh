#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
docker run --rm -v "$ROOT:/src" -w /src golang:1.26.6-alpine3.23 sh -ec \
  "find apps internal demo -name '*.go' -print0 | xargs -0 gofmt -d | tee /tmp/gofmt.diff; test ! -s /tmp/gofmt.diff"
