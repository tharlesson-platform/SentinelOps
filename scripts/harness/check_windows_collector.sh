#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
installer="$root/scripts/install-windows-collector.ps1"
config="$root/deploy/agents/windows/config.alloy.tmpl"
security="$root/deploy/agents/windows/security-eventlog.alloy.tmpl"
powershell_image='mcr.microsoft.com/powershell:7.5-ubuntu-24.04@sha256:042240d57ec9e47e511033b92625a8d95875ee5860af3015992c248b58a8be81'

for file in "$installer" "$config" "$security"; do
  test -s "$file" || { printf 'FAIL: artefato Windows ausente: %s\n' "$file" >&2; exit 1; }
done

for token in 'Get-FileHash -Algorithm SHA256' 'alloy validate' 'config.previous.alloy' 'Event Log Readers' 'Performance Monitor Users' 'IncludeSecurityEventLog' 'ConfirmRemove'; do
  grep -Fq "$token" "$installer" || { printf 'FAIL: controle Windows ausente: %s\n' "$token" >&2; exit 1; }
done

for token in 'prometheus.exporter.windows' 'loki.source.windowsevent "application"' 'loki.source.windowsevent "system"' 'bookmark_path' 'use_incoming_timestamp = true' 'min_version = "TLS12"'; do
  grep -Fq "$token" "$config" || { printf 'FAIL: contrato Alloy Windows ausente: %s\n' "$token" >&2; exit 1; }
done

for token in 'exclude_event_data     = true' 'exclude_event_message  = true' 'exclude_user_data      = true' 'bookmark_path'; do
  grep -Fq "$token" "$security" || { printf 'FAIL: proteção Security Event Log ausente: %s\n' "$token" >&2; exit 1; }
done

if grep -Ei 'Invoke-WebRequest.+https?://' "$installer" | grep -Evq '127\.0\.0\.1'; then
  printf 'FAIL: instalador Windows não pode baixar binários por URL arbitrária.\n' >&2
  exit 1
fi

docker run --rm -v "$root":/src:ro "$powershell_image" pwsh -NoProfile -Command '$tokens = $null; $errors = $null; [void][System.Management.Automation.Language.Parser]::ParseFile("/src/scripts/install-windows-collector.ps1", [ref]$tokens, [ref]$errors); if ($errors.Count -gt 0) { $errors | ForEach-Object { Write-Error $_.Message }; exit 1 }'

printf 'PASS: contrato estático e sintaxe PowerShell do collector Windows validados; execução em host Windows permanece pendente.\n'
