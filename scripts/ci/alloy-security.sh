#!/usr/bin/env bash
# Não inicia o sensor nem monta socket/arquivos do host no container.
set -euo pipefail
image=${1:?usage: alloy-security.sh IMAGE OUTPUT_DIR}
output=${2:?usage: alloy-security.sh IMAGE OUTPUT_DIR}
test ! -e "$output" # Never reuse reports from an earlier artifact/run.
mkdir -p "$output"
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
reader=
image_id=unavailable
finish() {
  result=$?
  trap - EXIT
  # Preserve raw diagnostics even if preparation/scanning failed.
  python3 "$script_dir/alloy-security.py" assess "$output" "$output/alloy" "$image_id" || result=1
  if test -n "$reader"; then docker rm "$reader" >/dev/null || result=1; fi
  exit "$result"
}
trap finish EXIT
image_id=$(docker image inspect "$image" --format '{{.Id}}')
test "$(docker image inspect "$image_id" --format '{{.Os}}/{{.Architecture}}')" = linux/amd64
reader=$(docker create --network none --platform linux/amd64 "$image_id")
docker cp "$reader:/usr/share/sentinelops/alloy-security/." "$output/"
docker cp "$reader:/bin/alloy" "$output/alloy"
docker image inspect "$image_id" > "$output/image-inspect.json"
# Capture exact config/manifest bytes from the immutable local image, not a tag.
# Streaming avoids retaining a second full image tar; pipefail retains export errors.
docker image save "$image_id" | python3 "$script_dir/alloy-security.py" \
  capture-identity "$output" "$output/alloy" "$image_id"
trivy image --scanners vuln --list-all-pkgs --ignore-unfixed=false --ignorefile /dev/null \
  --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL --exit-code 0 \
  --format json --output "$output/trivy-full.json" "$image_id"
trivy image --format cyclonedx --output "$output/trivy-sbom.json" "$image_id"
trivy --version > "$output/trivy-version.txt"
python3 "$script_dir/alloy-security.py" prepare "$output" "$output/alloy" "$image_id"
trivy sbom --scanners vuln --list-all-pkgs --ignore-unfixed=false --ignorefile /dev/null \
  --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL --exit-code 0 \
  --format json --output "$output/trivy-moby.json" "$output/moby-supplement.cdx.json"
