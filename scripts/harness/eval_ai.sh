#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
docker run --rm -v "$root:/src" -w /src \
  golang:1.26.10-bookworm@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb \
  go run ./apps/aieval ./harness/ai-evals/guardrails-v1.json
