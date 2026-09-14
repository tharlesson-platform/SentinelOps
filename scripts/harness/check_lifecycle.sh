#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
backup="$root/scripts/backup-local.sh"
restore="$root/scripts/restore-local.sh"

sh -n "$backup"
sh -n "$restore"
grep -Fq 'pause api worker agent temporal' "$backup"
grep -Fq 'unpause api worker agent temporal' "$backup"
grep -Fq 'age com scrypt' "$backup"
grep -Fq 'target-project deve terminar em -restore' "$restore"
grep -Fq 'target-project já possui recursos Docker Compose' "$restore"
grep -Fq 'validate --input /input/package.tar.gz' "$restore"
grep -Fq 'Valida tipos, paths, duplicatas e metadata antes de o tar tocar o filesystem' "$restore"
grep -Fq 'scope":"local-compose-isolated-restore' "$restore"
grep -Fq 'archiveSHA256' "$restore"
grep -Fq 'row-counts.txt' "$restore"
grep -Fq 'backup não testado por restore não atende ao gate de DR' "$backup"
printf 'PASS: backup/restore exige alvo Compose inédito, contrato autenticado e evidência local sanitizada.\n'
