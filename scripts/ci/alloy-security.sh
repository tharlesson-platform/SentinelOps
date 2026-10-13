#!/usr/bin/env bash
# Não inicia o sensor nem monta socket/arquivos do host no container.
set -euo pipefail
image=${1:?usage: alloy-security.sh IMAGE OUTPUT_DIR}
output=${2:?usage: alloy-security.sh IMAGE OUTPUT_DIR}
test ! -e "$output" # Never reuse reports from an earlier artifact/run.
mkdir -p "$output"
image_id=$(docker image inspect "$image" --format '{{.Id}}')
test "$(docker image inspect "$image" --format '{{.Os}}/{{.Architecture}}')" = linux/amd64
reader=$(docker create --network none --platform linux/amd64 "$image")
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
finish() {
  result=$?
  trap - EXIT
  # Preserve raw diagnostics even if preparation/scanning failed.
  python3 "$script_dir/alloy-security.py" assess "$output" "$output/alloy" "$image_id" || result=1
  docker rm "$reader" >/dev/null || result=1
  exit "$result"
}
trap finish EXIT
docker cp "$reader:/usr/share/sentinelops/alloy-security/." "$output/"
docker cp "$reader:/bin/alloy" "$output/alloy"
docker image inspect "$image" > "$output/image-inspect.json"
trivy image --scanners vuln --list-all-pkgs --ignore-unfixed=false --ignorefile /dev/null \
  --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL --exit-code 0 \
  --format json --output "$output/trivy-full.json" "$image"
trivy image --format cyclonedx --output "$output/trivy-sbom.json" "$image"
trivy --version > "$output/trivy-version.txt"
python3 "$script_dir/alloy-security.py" prepare "$output" "$output/alloy" "$image_id"
trivy sbom --scanners vuln --list-all-pkgs --ignore-unfixed=false --ignorefile /dev/null \
  --severity UNKNOWN,LOW,MEDIUM,HIGH,CRITICAL --exit-code 0 \
  --format json --output "$output/trivy-moby.json" "$output/moby-supplement.cdx.json"
python3 "$script_dir/alloy-security.py" assess "$output" "$output/alloy" "$image_id"
