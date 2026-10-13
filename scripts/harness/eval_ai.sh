#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
docker run --rm -v "$root:/src" -w /src \
  golang:1.26.9-bookworm@sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c \
  go run ./apps/aieval ./harness/ai-evals/guardrails-v1.json
