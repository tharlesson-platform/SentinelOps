#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
script="$root/scripts/bootstrap-apm.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM

sh -n "$script"
"$script" --language go --service-name api-hml --environment hml --team platform --owner platform \
  --otlp-endpoint http://127.0.0.1:4318 --output "$work"
test -s "$work/api-hml/.env.sentinelops"
test -s "$work/api-hml/service.json"
"$script" --language react --service-name portal-hml --environment hml --faro-endpoint https://faro.example.invalid/collect \
  --rum-sampling-rate 0.2 --output "$work"
grep -Fq 'sessionTracking: { samplingRate: 0.2 }' "$work/portal-hml/faro-config.ts"
grep -Fq "ignoreUrls: ['https://faro.example.invalid/collect']" "$work/portal-hml/faro-config.ts"
grep -Fq 'beforeSend(item)' "$work/portal-hml/faro-config.ts"
if "$script" --language go --service-name unsafe-http --otlp-endpoint http://collector.internal:4318 --output "$work" >/dev/null 2>&1; then
  printf 'FAIL: endpoint HTTP remoto foi aceito.\n' >&2
  exit 1
fi
if "$script" --language go --service-name unsafe-creds --otlp-endpoint https://user:secret@ingest.example.invalid:4318 --output "$work" >/dev/null 2>&1; then
  printf 'FAIL: credencial embutida no endpoint foi aceita.\n' >&2
  exit 1
fi
if "$script" --language react --service-name unsafe-faro --faro-endpoint https://faro.example.invalid/collect?token=x --output "$work" >/dev/null 2>&1; then
  printf 'FAIL: query no endpoint Faro foi aceita.\n' >&2
  exit 1
fi
if "$script" --language react --service-name unsafe-faro-string --faro-endpoint "https://faro.example.invalid/collect'" --output "$work" >/dev/null 2>&1; then
  printf 'FAIL: delimitador JavaScript no endpoint Faro foi aceito.\n' >&2
  exit 1
fi
if "$script" --language react --service-name unsafe-sampling --faro-endpoint https://faro.example.invalid/collect --rum-sampling-rate 0 --output "$work" >/dev/null 2>&1; then
  printf 'FAIL: sampling RUM zero foi aceito.\n' >&2
  exit 1
fi
printf 'PASS: onboarding APM restringe endpoints e gera RUM com sampling/redaction local.\n'
