#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
[ -f "$root/artifacts/runtime/docker-compose.images.lock.yml" ] || { echo 'BLOCKED: execute make prepare-images antes do E2E' >&2; exit 2; }
exec make -C "$root" test-synthetics
