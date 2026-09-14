#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
[ -f "$root/artifacts/runtime/docker-compose.images.lock.yml" ] || { echo 'BLOCKED: release exige lock de imagens' >&2; exit 2; }
exec make -C "$root" validate-release
