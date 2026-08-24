#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
CHART="$ROOT/deploy/helm/sentinelops"
KUBECONFORM_IMAGE=ghcr.io/yannh/kubeconform:v0.8.0-alpine@sha256:6b90a5f23d846140ce0194fe050b1995e546eba938f3a6bf10c039dd5e24588f
DIGEST=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

for dependency in docker helm jq ruby; do
  command -v "$dependency" >/dev/null 2>&1 || { echo "Dependencia ausente: $dependency" >&2; exit 2; }
done

evidence_dir="$ROOT/artifacts/resilience"
mkdir -p "$evidence_dir"
rendered="$evidence_dir/helm-distributed-$(date -u +%Y%m%dT%H%M%SZ).yaml"
umask 077

helm template sentinelops "$CHART" -f "$CHART/values-distributed-production.yaml" \
  --set global.oidcIssuerURL=https://id.example.com/realms/platform \
  --set global.allowedOrigin=https://sentinelops.example.com \
  --set 'global.releaseValidationAllowedHosts[0]=api.example.internal' \
  --set global.prometheusURL=https://prometheus.observability.svc \
  --set global.lokiURL=https://loki.observability.svc \
  --set global.tempoURL=https://tempo.observability.svc \
  --set gateway.prometheusUpstream=https://prometheus.observability.svc \
  --set gateway.lokiUpstream=https://loki.observability.svc \
  --set gateway.alloyHTTPUpstream=https://alloy-http.observability.svc \
  --set gateway.alloyGRPCUpstream=https://alloy-grpc.observability.svc \
  --set api.image.digest="$DIGEST" --set worker.image.digest="$DIGEST" \
  --set web.image.digest="$DIGEST" --set migration.image.digest="$DIGEST" \
  --set 'networkPolicy.externalHTTPSCIDRs[0]=10.10.0.0/24' \
  --set 'networkPolicy.gatewayIngressCIDRs[0]=10.20.0.0/24' > "$rendered"
chmod 600 "$rendered"

docker run --rm -i "$KUBECONFORM_IMAGE" -strict -summary -kubernetes-version 1.35.0 < "$rendered"

ruby -ryaml -e '
  docs = YAML.load_stream(File.read(ARGV[0])).compact
  deployments = docs.select { |d| d["kind"] == "Deployment" }
  expected = {"api"=>5, "worker"=>5, "web"=>3, "gateway"=>3}
  expected.each do |component, replicas|
    deployment = deployments.find { |d| d.dig("metadata", "labels", "app.kubernetes.io/component") == component }
    abort "deployment ausente: #{component}" unless deployment
    abort "replicas invalidas: #{component}" unless deployment.dig("spec", "replicas") == replicas
    pod_spec = deployment.dig("spec", "template", "spec")
    spread = pod_spec.fetch("topologySpreadConstraints").first
    abort "spread permissivo: #{component}" unless spread["whenUnsatisfiable"] == "DoNotSchedule"
    abort "anti-affinity ausente: #{component}" unless pod_spec.dig("affinity", "podAntiAffinity")
    image = pod_spec.fetch("containers").first.fetch("image")
    abort "imagem sem digest: #{component}" unless image.match?(/@sha256:[a-f0-9]{64}$/)
  end
  pdbs = docs.select { |d| d["kind"] == "PodDisruptionBudget" }
  abort "PDBs insuficientes" unless pdbs.length >= 4
  hpa = docs.find { |d| d["kind"] == "HorizontalPodAutoscaler" }
  abort "HPA ausente" unless hpa && hpa.dig("spec", "minReplicas") == 5 && hpa.dig("spec", "maxReplicas") == 30
  policies = docs.select { |d| d["kind"] == "NetworkPolicy" }
  abort "NetworkPolicies insuficientes" unless policies.length >= 6
' "$rendered"

for unsafe_environment in production prod PRD staging; do
  if SENTINEL_ENV=$unsafe_environment docker compose --env-file "$ROOT/.env" -f "$ROOT/deploy/compose/docker-compose.yml" run --rm --no-deps local-scope-guard >/dev/null 2>&1; then
    echo "Compose aceitou SENTINEL_ENV=$unsafe_environment." >&2
    exit 1
  fi
done

evidence_file="${rendered%.yaml}.json"
jq -n \
  --arg completedAt "$(date -u +%FT%TZ)" \
  --arg renderedManifest "$rendered" \
  '{schemaVersion:1,completedAt:$completedAt,profile:"distributed-production",replicas:{api:5,worker:5,web:3,gateway:3},topologySpread:"DoNotSchedule",podAntiAffinity:true,podDisruptionBudgets:true,hpa:{min:5,max:30},networkPolicies:true,imageDigestsRequired:true,kubeconform:true,composeProductionGuard:true,renderedManifest:$renderedManifest}' \
  > "$evidence_file"
chmod 600 "$evidence_file"

printf 'distributed_replicas=PASS\ntopology_spread=PASS\npod_anti_affinity=PASS\npdb=PASS\nhpa=PASS\nnetwork_policy=PASS\nimage_digests=PASS\nkubeconform=PASS\ncompose_production_guard=PASS\nevidence=%s\n' "$evidence_file"
