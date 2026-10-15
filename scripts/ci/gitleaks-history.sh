#!/usr/bin/env bash
# Retry applies only to acquiring this immutable image, never to the scanner.
set -euo pipefail
readonly image='ghcr.io/gitleaks/gitleaks:v8.30.0@sha256:691af3c7c5a48b16f187ce3446d5f194838f91238f27270ed36eef6359a574d9'
trap 'exit 130' INT
trap 'exit 143' TERM

for tool in docker timeout sleep; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'gitleaks: required local tool unavailable\n' >&2
    exit 127
  fi
done

for attempt in 1 2 3; do
  printf 'gitleaks: acquiring pinned image (attempt %s/3)\n' "$attempt" >&2
  # Registry errors may contain signed blob URLs or credentials across lines.
  # Suppress both streams entirely; log only constants and numeric exit status.
  if timeout --signal=TERM --kill-after=5s 120s docker pull "$image" >/dev/null 2>&1; then
    printf 'gitleaks: pinned image acquired; scanning once\n' >&2
    exec docker run --rm --pull=never -v "$PWD:/repo:ro" "$image" git /repo --redact --no-banner
  else
    status=$?
  fi
  printf 'gitleaks: pull failed (attempt %s/3, exit %s; registry output suppressed)\n' "$attempt" "$status" >&2
  # Cancellation/termination is not a transient registry failure.
  case "$status" in 130|137|143) exit "$status" ;; esac
  if [ "$attempt" -eq 3 ]; then
    exit "$status"
  fi
  sleep "$((attempt * 5))"
done
