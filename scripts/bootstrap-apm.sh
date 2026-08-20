#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
# shellcheck source=scripts/lib/linux-common.sh
. "$ROOT/scripts/lib/linux-common.sh"

LANGUAGE=""
SERVICE_NAME=""
SERVICE_NAMESPACE=default
SERVICE_VERSION=0.1.0
ENVIRONMENT=development
TEAM=platform
OWNER=platform
OTLP_ENDPOINT=http://127.0.0.1:4318
FARO_ENDPOINT=""
OUTPUT_ROOT="$ROOT/artifacts/onboarding"
FORCE=false

usage() {
  cat <<'EOF'
Uso: ./scripts/bootstrap-apm.sh --language LANGUAGE --service-name NAME [opções]

Linguagens: java, spring, quarkus, node, nestjs, python, fastapi, django,
            dotnet, go, react

  --namespace NAME
  --version VERSION
  --environment NAME
  --team NAME
  --owner NAME
  --otlp-endpoint URL
  --faro-endpoint URL          obrigatório para React
  --output DIRECTORY
  --force

O comando gera configuração, catálogo e instruções em artifacts/onboarding.
Ele não altera automaticamente o repositório da aplicação.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --language) LANGUAGE=$2; shift 2 ;;
    --service-name) SERVICE_NAME=$2; shift 2 ;;
    --namespace) SERVICE_NAMESPACE=$2; shift 2 ;;
    --version) SERVICE_VERSION=$2; shift 2 ;;
    --environment) ENVIRONMENT=$2; shift 2 ;;
    --team) TEAM=$2; shift 2 ;;
    --owner) OWNER=$2; shift 2 ;;
    --otlp-endpoint) OTLP_ENDPOINT=$2; shift 2 ;;
    --faro-endpoint) FARO_ENDPOINT=$2; shift 2 ;;
    --output) OUTPUT_ROOT=$2; shift 2 ;;
    --force) FORCE=true; shift ;;
    --help|-h) usage; exit 0 ;;
    *) die "Opção desconhecida: $1" ;;
  esac
done

[ -n "$LANGUAGE" ] || die "--language é obrigatório"
[ -n "$SERVICE_NAME" ] || die "--service-name é obrigatório"
case "$LANGUAGE" in java|spring|quarkus|node|nestjs|python|fastapi|django|dotnet|go|react) ;; *) die "Linguagem não suportada: $LANGUAGE" ;; esac
validate_name "$SERVICE_NAME" service-name
validate_name "$SERVICE_NAMESPACE" namespace
validate_name "$ENVIRONMENT" environment
validate_name "$TEAM" team
validate_name "$OWNER" owner
printf '%s' "$SERVICE_VERSION" | grep -Eq '^[a-zA-Z0-9][a-zA-Z0-9._+-]{0,63}$' || die "Versão inválida: $SERVICE_VERSION"
printf '%s' "$OTLP_ENDPOINT" | grep -Eq '^https?://[^[:space:]]+$' || die "OTLP endpoint inválido: $OTLP_ENDPOINT"
if [ "$LANGUAGE" = react ]; then
  [ -n "$FARO_ENDPOINT" ] || die "React requer --faro-endpoint; o perfil local ainda não publica um receiver Faro seguro."
  printf '%s' "$FARO_ENDPOINT" | grep -Eq '^https://[^[:space:]]+$' || die "Faro endpoint deve usar HTTPS: $FARO_ENDPOINT"
fi

OUTPUT_DIRECTORY="$OUTPUT_ROOT/$SERVICE_NAME"
if [ -e "$OUTPUT_DIRECTORY/.env.sentinelops" ] && [ "$FORCE" != true ]; then
  die "$OUTPUT_DIRECTORY já existe. Revise o conteúdo ou use --force conscientemente."
fi
mkdir -p "$OUTPUT_DIRECTORY"
umask 077

cat > "$OUTPUT_DIRECTORY/.env.sentinelops" <<EOF
OTEL_SERVICE_NAME=$SERVICE_NAME
OTEL_RESOURCE_ATTRIBUTES=service.namespace=$SERVICE_NAMESPACE,service.version=$SERVICE_VERSION,deployment.environment.name=$ENVIRONMENT,team=$TEAM,owner=$OWNER,application=$SERVICE_NAME
OTEL_EXPORTER_OTLP_ENDPOINT=$OTLP_ENDPOINT
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_TRACES_EXPORTER=otlp
OTEL_METRICS_EXPORTER=otlp
OTEL_LOGS_EXPORTER=otlp
OTEL_PROPAGATORS=tracecontext,baggage
OTEL_METRIC_EXPORT_INTERVAL=15000
EOF
chmod 600 "$OUTPUT_DIRECTORY/.env.sentinelops"

cat > "$OUTPUT_DIRECTORY/service.json" <<EOF
{
  "name": "$SERVICE_NAME",
  "displayName": "$SERVICE_NAME",
  "description": "Serviço $LANGUAGE instrumentado via OpenTelemetry",
  "ownerTeam": "$TEAM",
  "tier": "2",
  "labels": {
    "environment": "$ENVIRONMENT",
    "namespace": "$SERVICE_NAMESPACE",
    "language": "$LANGUAGE",
    "owner": "$OWNER"
  }
}
EOF

case "$LANGUAGE" in
  java|spring|quarkus)
    cat > "$OUTPUT_DIRECTORY/runtime.env.example" <<'EOF'
# Baixe e valide por checksum uma versão fixada do OpenTelemetry Java Agent.
JAVA_TOOL_OPTIONS=-javaagent:/opt/opentelemetry/opentelemetry-javaagent.jar
OTEL_INSTRUMENTATION_COMMON_DEFAULT_ENABLED=true
EOF
    LANGUAGE_GUIDE="Monte o Java Agent em /opt/opentelemetry, aplique runtime.env.example e reinicie uma instância canário. Spring Boot e Quarkus devem preservar a propagação W3C e nomes de rotas normalizados."
    ;;
  node|nestjs)
    cat > "$OUTPUT_DIRECTORY/instrumentation.mjs" <<'EOF'
// Carregue este módulo antes do código da aplicação, após fixar as dependências
// OpenTelemetry no lockfile do projeto.
import '@opentelemetry/auto-instrumentations-node/register';
EOF
    LANGUAGE_GUIDE="Fixe @opentelemetry/auto-instrumentations-node no lockfile e inicie o processo com node --import ./instrumentation.mjs. No NestJS, carregue antes do bootstrap do módulo raiz."
    ;;
  python|fastapi|django)
    cat > "$OUTPUT_DIRECTORY/run-with-otel.sh" <<'EOF'
#!/bin/sh
set -eu
# Instale e fixe opentelemetry-distro e os instrumentadores no lockfile.
exec opentelemetry-instrument "$@"
EOF
    chmod 700 "$OUTPUT_DIRECTORY/run-with-otel.sh"
    LANGUAGE_GUIDE="Fixe opentelemetry-distro/exporter-otlp no lockfile, execute opentelemetry-bootstrap -a install durante o build e use run-with-otel.sh antes de uvicorn, gunicorn ou manage.py."
    ;;
  dotnet)
    cat > "$OUTPUT_DIRECTORY/runtime.env.example" <<'EOF'
CORECLR_ENABLE_PROFILING=1
CORECLR_PROFILER={918728DD-259F-4A6A-AC2B-B85E1B658318}
DOTNET_ADDITIONAL_DEPS=/opt/opentelemetry/AdditionalDeps
DOTNET_SHARED_STORE=/opt/opentelemetry/store
DOTNET_STARTUP_HOOKS=/opt/opentelemetry/net/OpenTelemetry.AutoInstrumentation.StartupHook.dll
EOF
    LANGUAGE_GUIDE="Instale uma versão fixada do .NET Auto Instrumentation no artefato da aplicação, aplique runtime.env.example e valide uma instância canário antes de ampliar o rollout."
    ;;
  go)
    cat > "$OUTPUT_DIRECTORY/resource.go.example" <<'EOF'
// Configure um sdk/resource com semconv.ServiceNameKey e exportadores OTLP.
// Passe o context da request e encerre providers com Shutdown(ctx) no graceful shutdown.
EOF
    LANGUAGE_GUIDE="Adicione OpenTelemetry SDK/exporters ao go.mod, inicialize providers de trace/metric antes do servidor e valide graceful shutdown. Auto-instrumentação eBPF é opcional e exige avaliação de kernel/capabilities."
    ;;
  react)
    cat > "$OUTPUT_DIRECTORY/faro-config.ts" <<EOF
// Fixe @grafana/faro-web-sdk e @grafana/faro-web-tracing no lockfile.
import { initializeFaro, getWebInstrumentations } from '@grafana/faro-web-sdk';
import { TracingInstrumentation } from '@grafana/faro-web-tracing';

export const faro = initializeFaro({
  url: '$FARO_ENDPOINT',
  app: { name: '$SERVICE_NAME', version: '$SERVICE_VERSION', environment: '$ENVIRONMENT' },
  instrumentations: [...getWebInstrumentations(), new TracingInstrumentation()],
});
EOF
    LANGUAGE_GUIDE="Publique o receiver Faro em endpoint dedicado com CORS restrito. Não envie tokens, corpo de formulário, email ou identificadores pessoais. Valide Web Vitals e correlação com o backend em um ambiente não produtivo."
    ;;
esac

cat > "$OUTPUT_DIRECTORY/README.md" <<EOF
# Onboarding APM: $SERVICE_NAME

## Aplicação

- Linguagem/framework: $LANGUAGE
- Namespace: $SERVICE_NAMESPACE
- Versão inicial: $SERVICE_VERSION
- Ambiente: $ENVIRONMENT
- Time/owner: $TEAM / $OWNER
- OTLP: $OTLP_ENDPOINT

## Aplicação da configuração

$LANGUAGE_GUIDE

1. Copie as variáveis de \`.env.sentinelops\` para o secret/configuração do runtime; não versione credenciais.
2. Fixe as bibliotecas no lockfile da aplicação e registre o checksum de qualquer agente binário.
3. Faça rollout em uma instância canário.
4. Gere uma request com \`traceparent\` e confirme métrica, log e trace pelo mesmo \`service.name\`.
5. Confirme que rotas dinâmicas foram normalizadas e que dados pessoais não aparecem em atributos ou labels.
6. Registre o catálogo: \`sentinelctl service apply -f service.json\`.
7. Só amplie o rollout após 15 minutos sem erro de exportação nem aumento material de CPU, memória ou latência.

## Rollback

Remova o loader/agent e as variáveis OTEL da configuração da aplicação, reverta o rollout e confirme que o processo volta ao baseline. A remoção da instrumentação não deve remover dados históricos.
EOF

log "Kit APM criado em $OUTPUT_DIRECTORY"
log "Revise README.md e .env.sentinelops antes de alterar a aplicação."
