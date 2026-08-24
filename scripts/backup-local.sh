#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/lib/linux-common.sh
. "$ROOT/scripts/lib/linux-common.sh"

OUTPUT_DIR="$ROOT/artifacts/backups"
PASSPHRASE_FILE=""
PROJECT_NAME=sentinelops

usage() {
  cat <<'EOF'
Uso: backup-local.sh --passphrase-file FILE [--output DIR] [--project-name NAME]

Pausa temporariamente API, worker, agente e Temporal para obter consistência
entre bancos e objetos. Gera backup de sentinel, temporal e temporal_visibility,
espelha o bucket sentinel-artifacts, registra checksums e cifra/autentica o
pacote no formato age com scrypt.
O arquivo de senha deve existir com modo 0600 e nunca é copiado ao backup.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) OUTPUT_DIR=$2; shift 2 ;;
    --passphrase-file) PASSPHRASE_FILE=$2; shift 2 ;;
    --project-name) PROJECT_NAME=$2; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

[ -s "$PASSPHRASE_FILE" ] || die "--passphrase-file válido é obrigatório"
permissions=$(stat -c '%a' "$PASSPHRASE_FILE" 2>/dev/null || stat -f '%Lp' "$PASSPHRASE_FILE")
[ "$permissions" = 600 ] || die "passphrase-file deve ter modo 0600"
validate_name "$PROJECT_NAME" project-name
command_exists sha256sum || command_exists shasum || die "sha256sum ou shasum é obrigatório"

compose=$(compose_command "$ROOT")
docker_prefix=$(docker_command)
crypto_image=sentinelops-backupcrypt:1.3.1
log "Construindo helper de backup autenticado a partir de fontes e bases fixadas"
# shellcheck disable=SC2086
$docker_prefix build -q -f "$ROOT/Dockerfile.app" --build-arg APP=backupcrypt -t "$crypto_image" "$ROOT" >/dev/null
mkdir -p "$OUTPUT_DIR"
chmod 700 "$OUTPUT_DIR"
staging=$(mktemp -d "${OUTPUT_DIR}/.backup.XXXXXX")
quiesced=false
cleanup() {
  if [ "$quiesced" = true ]; then
    # shellcheck disable=SC2086
    $compose -p "$PROJECT_NAME" unpause api worker agent temporal >/dev/null 2>&1 || true
  fi
  rm -rf "$staging"
}
trap cleanup EXIT HUP INT TERM
mkdir -p "$staging/databases" "$staging/minio"

log "Pausando writers para snapshot lógico consistente"
# shellcheck disable=SC2086
$compose -p "$PROJECT_NAME" pause api worker agent temporal
quiesced=true

log "Exportando bancos PostgreSQL com snapshot consistente por banco"
for database in sentinel temporal temporal_visibility; do
  # shellcheck disable=SC2086
  $compose -p "$PROJECT_NAME" exec -T postgres pg_dump -U sentinel --format=custom --no-owner --no-acl "$database" > "$staging/databases/$database.dump"
done
# shellcheck disable=SC2086
$compose -p "$PROJECT_NAME" exec -T postgres pg_dumpall -U sentinel --globals-only --no-role-passwords > "$staging/databases/globals.sql"

log "Espelhando objetos do MinIO sem expor credenciais no host"
# shellcheck disable=SC2086,SC2016
$compose -p "$PROJECT_NAME" run --rm -T -v "$staging/minio:/backup" --entrypoint /bin/sh minio-init -ec \
  'mc alias set source http://minio:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null && mc mirror --overwrite source/sentinel-artifacts /backup/sentinel-artifacts'

git_revision=$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || printf unknown)
created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
cat > "$staging/metadata.json" <<EOF
{"schemaVersion":1,"createdAt":"$created_at","sourceProject":"$PROJECT_NAME","gitRevision":"$git_revision","databases":["sentinel","temporal","temporal_visibility"],"bucket":"sentinel-artifacts"}
EOF

if command_exists sha256sum; then
  (cd "$staging" && find . -type f ! -name SHA256SUMS -print | LC_ALL=C sort | xargs sha256sum) > "$staging/SHA256SUMS"
else
  (cd "$staging" && find . -type f ! -name SHA256SUMS -print | LC_ALL=C sort | xargs shasum -a 256) > "$staging/SHA256SUMS"
fi

tar -C "$staging" -czf "$staging/package.tar.gz" databases minio metadata.json SHA256SUMS
archive="$OUTPUT_DIR/sentinelops-backup-$(date -u +%Y%m%dT%H%M%SZ).tar.gz.age"
# O container roda com o UID/GID do operador para ler somente o secret 0600 e
# criar o arquivo final sem ampliar permissões no host.
# shellcheck disable=SC2086
$docker_prefix run --rm --network none --user "$(id -u):$(id -g)" \
  -v "$staging:/input:ro" -v "$OUTPUT_DIR:/output" -v "$PASSPHRASE_FILE:/run/secrets/passphrase:ro" \
  "$crypto_image" encrypt --input /input/package.tar.gz --output "/output/$(basename "$archive")" --passphrase-file /run/secrets/passphrase
chmod 600 "$archive"
# shellcheck disable=SC2086
$compose -p "$PROJECT_NAME" unpause api worker agent temporal
quiesced=false
log "Backup cifrado criado: $archive"
log "Valide com scripts/restore-local.sh em projeto isolado; backup não testado por restore não atende ao gate de DR."
