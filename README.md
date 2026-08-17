# SentinelOps

Plataforma self-hosted de observabilidade, testes sintéticos e garantia de
delivery. O SentinelOps combina um Control Plane próprio com OpenTelemetry e o
stack Grafana, mantendo gates determinísticos e evidências auditáveis.

> Estado: **functional local MVP**. O caminho local é executável; os artefatos
> Kubernetes/Terraform são referências de produção e nunca são aplicados
> automaticamente. Consulte [Limitações reais](#limitações-reais).

## O que funciona

- API Go versionada, PostgreSQL/migrations, JWT local, RBAC e audit events.
- Service Catalog, agentes com bootstrap/heartbeat, cenários versionados e SSE.
- Workflows Temporal para releases e gate HTTP com PASS/FAIL/INCONCLUSIVE.
- `sentinelctl` para login, contexts, doctor, serviços, cenários, releases,
  validações e agentes, com saída human/JSON/YAML e códigos de saída de CI.
- UI React pt-BR/en-US, dark/light, Catálogo, Test Studio, Delivery Assurance,
  Agent Fleet e conteúdo didático.
- Prometheus, Loki, Tempo, Pyroscope, Grafana, Alloy, Blackbox e MinIO reais.
- Demo instrumentada com métricas, logs OTLP, traces, pprof/profiles e falhas
  controladas; Playwright e k6 reais com artefatos.
- 35 dashboards Grafana gerenciados, alertas de self-monitoring e drill-down de
  metric/exemplar → trace → logs → profile configurado.
- Helm hardened, External Secrets/Argo CD, Terraform para storage/PostgreSQL e
  exemplos de GitHub, GitLab, Jenkins, Azure DevOps, CodePipeline e Rollouts.

## Arquitetura

Consulte [overview](docs/architecture/overview.md),
[versões](docs/architecture/component-versions.md),
[ADRs](docs/architecture/decisions/) e
[threat model](docs/security/threat-model.md).

```text
Web / sentinelctl / CI ──REST/SSE──> API ──> PostgreSQL
                                    │
                                    └──> Temporal ──> workers/agentes
Apps/infra ──OTLP──> Alloy ──> Prometheus | Loki | Tempo | Pyroscope
                                      Grafana <───────────────┘
```

## Pré-requisitos

- Docker Desktop com Compose 5.3+ e pelo menos 8 GiB disponíveis.
- `make`, `openssl`, `htpasswd`, `curl` e `jq`.
- macOS Apple Silicon, Linux arm64 ou Linux amd64.

## Quickstart

```bash
make bootstrap
make up
make seed
make doctor
```

`make bootstrap` gera secrets e a senha local em `.env` com modo `0600`. A
senha é exibida apenas no terminal; para recuperá-la conscientemente:

```bash
make credentials
```

Não copie `.env` para CI, commits, tickets ou documentação.

## URLs e portas locais

| Serviço | URL | Exposição |
|---|---|---|
| SentinelOps Web | <http://localhost:3000> | loopback |
| SentinelOps API/OpenAPI | <http://localhost:8080> | loopback |
| Grafana | <http://localhost:3001> | loopback |
| Temporal UI | <http://localhost:8088> | loopback |
| Keycloak | <http://localhost:8081> | loopback |
| Mailpit | <http://localhost:8025> | loopback |
| MinIO API/Console | <http://localhost:9000>, <http://localhost:9001> | loopback |
| Prometheus | <http://localhost:9090> | loopback |
| Loki | <http://localhost:3100> | loopback |
| Tempo | <http://localhost:3200> | loopback |
| Pyroscope | <http://localhost:4040> | loopback |
| Alloy | <http://localhost:12345> | loopback |
| Demo API | <http://localhost:8090> | loopback |
| OTLP gRPC/HTTP | `4317`, `4318` | loopback |

## Operação local

```bash
make up              # build e start idempotente
make down            # para, preservando volumes
make reset           # remove somente volumes do projeto local
make seed             # registra demo e cenário HTTP
make test             # Go race tests, web tests e build
make test-synthetics  # Playwright e k6 reais
make logs             # logs agregados do Compose
make doctor           # health/readiness ponta a ponta
```

## CLI

Build sem instalar Go localmente:

```bash
docker build -f Dockerfile.app --build-arg APP=cli -t sentinelctl:0.1.0 .
docker run --rm --network host -e SENTINEL_API_URL=http://localhost:8080 \
  -e SENTINEL_PASSWORD='<senha local>' sentinelctl:0.1.0 login
```

Em hosts com Go:

```bash
go run ./apps/cli --output json doctor
SENTINEL_PASSWORD='<senha>' go run ./apps/cli login
go run ./apps/cli service list
go run ./apps/cli scenario apply -f examples/scenarios/demo-health.json
```

Códigos: `0` sucesso, `2` gate FAIL, `3` erro de execução, `4`
INCONCLUSIVE e `5` autenticação/autorização.

## Registrar uma aplicação

1. Instrumente a aplicação com OTLP para `http://alloy:4318` ou gRPC `4317`.
2. Use os atributos mínimos: `service.name`, `service.version`,
   `deployment.environment.name`, `team` e `owner`.
3. Normalize rotas e nunca use `user_id`, email, token, request/trace ID como
   label métrica.
4. Registre o catálogo:

```bash
sentinelctl service apply -f service.yaml
```

5. Confirme recebimento no Grafana, crie SLO/cenário e associe uma política de
   release antes de usar o gate em PRD.

## Validar um deployment

```bash
sentinelctl release register \
  --service sentinel-demo-api --environment development \
  --version 1.0.0 --commit-sha "$GIT_SHA" --image-digest "$IMAGE_DIGEST"

sentinelctl release validate --release-id "$RELEASE_ID" --mode standard
sentinelctl validation wait "$VALIDATION_ID"
```

Exemplos completos estão em [examples/integrations](examples/integrations/).
O rollback automático é desabilitado; qualquer adapter exige política,
escopo, auditoria, dry-run e aprovação explícita.

## Falhas controladas da demo

Somente a demo aceita `?fault=latency|timeout|error|cpu|exception` ou o header
`X-Demo-Fault`. Não há operações destrutivas. Exemplo:

```bash
curl 'http://localhost:8090/health?fault=error'
curl -H 'X-Demo-Fault: latency' http://localhost:8090/api/orders
```

## Produção

- Use `deploy/helm/sentinelops`, `values-small-production.yaml` ou
  `values-distributed-production.yaml`; publique imagens por digest após scan,
  SBOM e assinatura.
- Use OIDC, TLS/mTLS, External Secrets, PostgreSQL/Temporal/object storage HA,
  backups testados e NetworkPolicies ajustadas ao cluster real.
- Rode `helm lint/template` e `terraform plan`; **não** use Compose em produção.
- Loki produtivo grande deve usar microservices; Mimir substitui Prometheus.
- Consulte [produção](docs/operations/production.md),
  [backup/restore/upgrade](docs/operations/lifecycle.md) e
  [runbooks](docs/runbooks/operational-response.md).

## Limitações reais

- A API implementa descoberta OIDC e validação JWKS genérica, incluindo claims
  de grupos e roles do Keycloak. A federação real com Entra ID/Keycloak, MFA e
  os mapeamentos de grupos de cada organização ainda exigem homologação no IdP.
- Worker executa HTTP sintético periódico; execução distribuída browser/k6 via
  fila de agentes, importadores cURL/OpenAPI/Postman/HAR, plugins AMQP/Kafka/DB,
  flaky detection e cache offline ainda não estão implementados.
- O gate funcional avalia o check HTTP. Queries determinísticas PromQL/LogQL/
  TraceQL, baseline estatística, SLO e infraestrutura estão modeladas, mas não
  participam ainda do motor executável.
- Dashboard Studio próprio, incident management, capacity/correlation agents,
  RUM/Faro e assistant read-only permanecem backlog.
- Helm não instala os backends de observabilidade; opere charts oficiais
  versionados por perfil. Terraform inclui storage/RDS, não provisiona ainda
  EKS/AKS, Mimir/Loki/Tempo/Pyroscope/Temporal HA nem DR multi-região.
- O Compose é single-node, sem mTLS interno ou HA. Evidência local não equivale
  a aprovação de produção.

## Licenças

Código próprio: Apache-2.0. Consulte [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md),
[component versions](docs/architecture/component-versions.md) e gere SBOM no CI.
