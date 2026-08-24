#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
REPO_URL=""
REVISION=""
VALUES_FILE=""
OUTPUT=""
PROJECT=platform
DESTINATION_SERVER=https://kubernetes.default.svc
DESTINATION_NAMESPACE=sentinelops

usage() {
  cat <<'EOF'
Uso: render-argocd-application.sh --repo-url HTTPS_GIT --revision COMMIT_SHA \
  --values-file FILE --output FILE [--project NAME] [--destination-server URL] \
  [--destination-namespace NAME]

Valida o chart com values produtivos e gera uma Application presa a commit SHA.
O arquivo de saída não pode existir.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --repo-url) REPO_URL=$2; shift 2 ;;
    --revision) REVISION=$2; shift 2 ;;
    --values-file) VALUES_FILE=$2; shift 2 ;;
    --output) OUTPUT=$2; shift 2 ;;
    --project) PROJECT=$2; shift 2 ;;
    --destination-server) DESTINATION_SERVER=$2; shift 2 ;;
    --destination-namespace) DESTINATION_NAMESPACE=$2; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) printf 'Opção desconhecida: %s\n' "$1" >&2; exit 2 ;;
  esac
done

printf '%s' "$REPO_URL" | grep -Eq '^https://[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)+\.git$' || {
  echo "--repo-url deve ser URL HTTPS de repositório Git" >&2; exit 2;
}
printf '%s' "$REVISION" | grep -Eq '^[a-f0-9]{40}$' || {
  echo "--revision deve ser commit SHA completo de 40 caracteres" >&2; exit 2;
}
printf '%s' "$VALUES_FILE" | grep -Eq '^[A-Za-z0-9][A-Za-z0-9._/-]*\.ya?ml$' || {
  echo "--values-file inválido" >&2; exit 2;
}
case "$VALUES_FILE" in *..*|/*) echo "--values-file deve permanecer dentro do chart" >&2; exit 2 ;; esac
printf '%s' "$OUTPUT" | grep -Eq '^/?[A-Za-z0-9._/-]+\.ya?ml$' || { echo "--output inválido" >&2; exit 2; }
printf '%s' "$PROJECT" | grep -Eq '^[a-z0-9][a-z0-9-]{1,62}$' || { echo "--project inválido" >&2; exit 2; }
printf '%s' "$DESTINATION_NAMESPACE" | grep -Eq '^[a-z0-9][a-z0-9-]{1,62}$' || { echo "namespace inválido" >&2; exit 2; }
printf '%s' "$DESTINATION_SERVER" | grep -Eq '^https://[A-Za-z0-9._:-]+(/[A-Za-z0-9._~:/?%&=+-]*)?$' || { echo "destination-server deve usar URL HTTPS segura" >&2; exit 2; }

CHART="$ROOT/deploy/helm/sentinelops"
VALUES_PATH="$CHART/$VALUES_FILE"
[ -f "$VALUES_PATH" ] && [ ! -L "$VALUES_PATH" ] || { echo "values produtivo ausente ou symlink recusado: $VALUES_PATH" >&2; exit 1; }
VALUES_PARENT=$(CDPATH='' cd -- "$(dirname -- "$VALUES_PATH")" && pwd -P)
VALUES_PATH="$VALUES_PARENT/$(basename -- "$VALUES_PATH")"
case "$VALUES_PATH" in "$CHART"/*) ;; *) echo "values deve permanecer fisicamente dentro do chart" >&2; exit 1 ;; esac
[ ! -e "$OUTPUT" ] || { echo "saída já existe: $OUTPUT" >&2; exit 1; }
command -v helm >/dev/null 2>&1 || { echo "helm é obrigatório" >&2; exit 1; }
helm lint "$CHART" -f "$VALUES_PATH" >/dev/null
RENDERED=$(mktemp)
trap 'rm -f "$RENDERED"' EXIT HUP INT TERM
helm template sentinelops "$CHART" -f "$VALUES_PATH" > "$RENDERED"
grep -q 'sentinelops.io/production-gate: "true"' "$RENDERED" || {
  echo "values recusado: global.requireImageDigests=true é obrigatório" >&2
  exit 1
}

mkdir -p "$(dirname -- "$OUTPUT")"
umask 077
cat > "$OUTPUT" <<EOF
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: sentinelops
  namespace: argocd
spec:
  project: $PROJECT
  source:
    repoURL: "$REPO_URL"
    targetRevision: "$REVISION"
    path: deploy/helm/sentinelops
    helm:
      valueFiles: ["$VALUES_FILE"]
  destination:
    server: "$DESTINATION_SERVER"
    namespace: $DESTINATION_NAMESPACE
  syncPolicy:
    automated: { prune: false, selfHeal: true }
    syncOptions: [CreateNamespace=true, ServerSideApply=true]
    retry: { limit: 5, backoff: { duration: 5s, factor: 2, maxDuration: 3m } }
EOF
chmod 0600 "$OUTPUT"
echo "Application validada e gerada em $OUTPUT"
