#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
docker run --rm -v "$root:/src" -w /src \
  golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 \
  go run ./apps/aieval ./harness/ai-evals/guardrails-v1.json
