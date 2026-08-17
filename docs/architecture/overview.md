# Arquitetura do SentinelOps

SentinelOps separa o **Control Plane**, que armazena intenção, agenda jobs e
avalia políticas, do **Data Plane**, que coleta e consulta telemetria.

```mermaid
flowchart LR
  U["Web / sentinelctl / CI"] -->|REST, SSE| API["Control Plane API"]
  API --> PG[(PostgreSQL)]
  API --> T[Temporal]
  W["Workers por fila"] --> T
  A["Agentes privados"] -->|mTLS em produção| API
  W --> OBJ[(S3 / MinIO)]
  APP["Apps e infraestrutura"] --> AL[Grafana Alloy]
  AL --> P[Prometheus / Mimir]
  AL --> L[Loki]
  AL --> TP[Tempo]
  AL --> PY[Pyroscope]
  G[Grafana] --> P & L & TP & PY
  API --> G
```

## Control Plane

- Go, API REST em `/api/v1`, OpenAPI 3.1 e SSE.
- PostgreSQL com isolamento lógico por `organization_id`, constraints e
  auditoria append-only.
- Temporal para workflows duráveis. Redis não é dependência obrigatória.
- Política determinística versionada para gates.
- Autenticação local somente em desenvolvimento; OIDC é a opção de produção.

## Data Plane

- OTLP gRPC/HTTP é o contrato de transporte.
- Alloy recebe, remove atributos sensíveis, aplica limites e encaminha sinais.
- `local-demo`: Prometheus, Loki monolítico, Tempo monolítico e Pyroscope.
- Produção distribuída: Mimir, Loki microservices, Tempo e Pyroscope com object
  storage. Simple Scalable Loki não é tratado como arquitetura final.

## Fluxo de validação

```mermaid
sequenceDiagram
  participant CI
  participant API
  participant Temporal
  participant Agent
  participant Telemetry
  CI->>API: POST /releases (Idempotency-Key)
  CI->>API: POST /releases/{id}/validate
  API->>Temporal: start workflow
  Temporal->>Agent: synthetic jobs
  Agent-->>Temporal: assertions + artifacts
  Temporal->>Telemetry: deterministic bounded queries
  Temporal-->>API: checks + evidence
  API-->>CI: PASS/WARN/FAIL/INCONCLUSIVE
```

## Limites de confiança

Agentes nunca recebem shell arbitrário. Secrets são referências resolvidas no
ambiente de execução. Logs, traces, uploads e respostas externas são conteúdo
não confiável. Assistentes opcionais só geram consultas allowlisted e nunca
alteram o resultado do gate.

