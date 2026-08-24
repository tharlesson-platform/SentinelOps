#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
IMAGE=${ALLOY_PATCHED_IMAGE:-sentinelops-alloy:1.18.1-patched.1}
TRIVY_IMAGE=aquasec/trivy:0.73.0@sha256:7cced7cae583819fc7806d4cbc0dbbc7cad18b99f7d3e235192e6da8c091045c
OUTPUT_DIR=${SECURITY_ARTIFACT_DIR:-$ROOT/artifacts/security}
mkdir -p "$OUTPUT_DIR"

docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v sentinelops-trivy-cache:/root/.cache \
  "$TRIVY_IMAGE" image --scanners vuln --severity HIGH,CRITICAL --ignore-unfixed \
  --exit-code 1 --skip-version-check "$IMAGE"

docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v sentinelops-trivy-cache:/root/.cache \
  -v "$OUTPUT_DIR:/output" "$TRIVY_IMAGE" image --format cyclonedx --output /output/alloy.cdx.json \
  --skip-version-check "$IMAGE"

docker image inspect "$IMAGE" --format '{{index .RepoDigests 0}}' > "$OUTPUT_DIR/alloy-image-digest.txt"
chmod 0644 "$OUTPUT_DIR/alloy.cdx.json" "$OUTPUT_DIR/alloy-image-digest.txt"
echo "Scan sem HIGH/CRITICAL e SBOM CycloneDX em $OUTPUT_DIR"
