#!/usr/bin/env bash
set -euo pipefail

# Test only the loaded image, without a production API or credentials.
image=${1:?Pass the locally loaded web image}
test "$(docker image inspect "$image" --format '{{.Os}}/{{.Architecture}}')" = linux/amd64
test "$(docker image inspect "$image" --format '{{.Config.User}}')" = nginx
container=
cleanup() {
  if [[ -n "$container" ]]; then
    docker rm -f "$container" >/dev/null
  fi
}
trap cleanup EXIT
container=$(docker run -d --platform linux/amd64 -p 127.0.0.1::3000 "$image")
port=$(docker port "$container" 3000/tcp)
[[ "$port" =~ ^127\.0\.0\.1:[0-9]+$ ]]
base="http://$port"
html=
for ((attempt = 0; attempt < 30; attempt++)); do
  if html=$(curl --noproxy 127.0.0.1 --fail --silent --show-error --max-time 2 "$base/"); then
    break
  fi
  sleep 1
done
test -n "$html"
[[ "$html" == *'<!doctype html>'* ]]
test "$(docker inspect "$container" --format '{{.State.Running}}')" = true
docker exec "$container" sh -c 'test "$(id -u)" != 0; test -s /tmp/nginx.pid'
for extension in js css; do
  asset=$(printf '%s\n' "$html" | grep -oE "/assets/[A-Za-z0-9._-]+\.$extension" | head -n 1)
  [[ "$asset" =~ ^/assets/[A-Za-z0-9._-]+\.(js|css)$ ]]
  response=$(curl --noproxy 127.0.0.1 --fail --silent --show-error --max-time 10 --output /dev/null \
    --write-out '%{size_download} %{content_type}' "$base$asset")
  size=${response%% *}
  content_type=${response#* }
  [[ "$size" =~ ^[0-9]+$ ]] && ((size > 0))
  case "$extension:$content_type" in
    js:application/javascript*|js:text/javascript*|css:text/css*) ;;
    *) echo "Unexpected asset content type: $extension:$content_type" >&2; exit 1 ;;
  esac
done
echo 'PASS: AMD64 web image, non-root nginx, PID, HTTP, JavaScript and CSS'
