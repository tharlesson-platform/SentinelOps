#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
ACTIONLINT_IMAGE=docker.io/rhysd/actionlint:1.7.7@sha256:887a259a5a534f3c4f36cb02dca341673c6089431057242cdc931e9f133147e9

for dependency in docker git jq; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependencia ausente: $dependency" >&2; exit 2; }
done

docker run --rm -v "$ROOT:/repo" -w /repo "$ACTIONLINT_IMAGE"

release_file="$ROOT/.github/workflows/release.yml"
grep -q 'environment: production' "$release_file"
grep -q 'id-token: write' "$release_file"
grep -q 'cosign sign ' "$release_file"
grep -q 'cosign verify ' "$release_file"
# O texto literal abaixo valida a referencia por digest no workflow.
# shellcheck disable=SC2016
grep -q 'image@$digest' "$release_file"
grep -q 'gh release create' "$release_file"
grep -q 'requireImageDigests: true' "$ROOT/deploy/helm/sentinelops/values-small-production.yaml"
grep -q 'requireImageDigests: true' "$ROOT/deploy/helm/sentinelops/values-distributed-production.yaml"

"$ROOT/scripts/sign-local-image-lock.sh" >/dev/null
latest_signature=$(find "$ROOT/artifacts/release" -mindepth 1 -maxdepth 1 -type d -name 'local-*' | sort | tail -1)
test -n "$latest_signature"

remote_status=NOT_CONFIGURED
remote_url=$(git -C "$ROOT" remote get-url origin 2>/dev/null || true)
[ -z "$remote_url" ] || remote_status=CONFIGURED_NOT_VERIFIED

evidence_dir="$ROOT/artifacts/release"
mkdir -p "$evidence_dir"
evidence_file="$evidence_dir/controls-$(date -u +%Y%m%dT%H%M%SZ).json"
umask 077
jq -n \
  --arg completedAt "$(date -u +%FT%TZ)" \
  --arg commit "$(git -C "$ROOT" rev-parse HEAD)" \
  --arg remoteStatus "$remote_status" \
  --arg remoteURL "$remote_url" \
  --arg signatureEvidence "$latest_signature" \
  '{schemaVersion:1,completedAt:$completedAt,commit:$commit,localControls:{actionlint:true,productionEnvironmentGate:true,oidcKeylessSigning:true,digestPromotion:true,helmDigestGate:true,localImmutableManifestSignedAndVerified:true},remote:{status:$remoteStatus,url:$remoteURL},signatureEvidence:$signatureEvidence}' \
  > "$evidence_file"
chmod 600 "$evidence_file"

printf 'actionlint=PASS\nproduction_environment_gate=PASS\nkeyless_signing_workflow=PASS\ndigest_promotion=PASS\nlocal_manifest_signature=PASS\nremote_status=%s\nevidence=%s\n' "$remote_status" "$evidence_file"
