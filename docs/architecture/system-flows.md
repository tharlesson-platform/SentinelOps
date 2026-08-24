# Fluxos do sistema

Os diagramas abaixo mostram responsabilidades e limites entre instalação,
coleta, processamento, consulta e promoção. A documentação é a referência; o
estado vivo deve sempre ser comprovado no ambiente alvo.

## Instalação do zero em Linux

```mermaid
flowchart TD
  Start[Servidor Linux vazio] --> Preflight[Preflight: CPU, memória, disco, portas e ferramentas]
  Preflight --> Runtime[Docker Engine e Compose v2]
  Runtime --> Source[Download HTTPS e validação SHA256]
  Source --> Secrets[Secrets locais e PKI com permissões restritas]
  Secrets --> Build[Build e trava das imagens por SHA256]
  Build --> Migrate[Migrações com role dedicada]
  Migrate --> Stack[Control plane, backends, mocks e gateway]
  Stack --> Service[Systemd ou OpenRC]
  Service --> Doctor[Health e readiness]
  Doctor --> E2E[Prova métrica, log, trace, profile e gates]
  E2E --> Ready[Ambiente single-node funcional]
```

## Coleta de um servidor Linux

```mermaid
sequenceDiagram
  participant Admin as Administrador
  participant PKI as CA offline
  participant Host as Host monitorado
  participant Gateway as Gateway mTLS
  participant Alloy as Alloy central
  participant Backends as Prometheus e Loki
  Admin->>PKI: emite identidade SPIFFE do host
  PKI-->>Admin: CA pública, certificado e chave do cliente
  Admin->>Host: instala bundle sem chave da CA
  Host->>Gateway: métricas, logs e OTLP com mTLS
  Gateway->>Gateway: valida SAN, tenant e limites
  Gateway->>Alloy: encaminha sinal autorizado
  Alloy->>Backends: processa e persiste
  Backends-->>Admin: séries e logs nos dashboards de hosts
```

## APM de uma aplicação

```mermaid
flowchart LR
  App[Aplicação instrumentada] -->|OTLP local ou mTLS remoto| Ingest[Gateway / Alloy]
  Ingest --> Filter[Redação, limites e atributos de recurso]
  Filter --> Metrics[Prometheus / Mimir]
  Filter --> Logs[Loki]
  Filter --> Traces[Tempo]
  App --> Profiles[Pyroscope]
  Metrics & Logs & Traces & Profiles --> Grafana[Dashboards e investigação]
  Catalog[Service Catalog] --> Grafana
```

Os atributos mínimos são `service.name`, `service.version`,
`deployment.environment.name`, `team` e `owner`. Dados pessoais, tokens e IDs
de alta cardinalidade não podem ser labels métricas.

## Validação de uma release

```mermaid
sequenceDiagram
  participant CI
  participant API
  participant Temporal
  participant Agent
  participant Telemetry
  CI->>API: registra SHA e digest da release
  CI->>API: solicita validação idempotente
  API->>Temporal: inicia workflow versionado
  Temporal->>Agent: executa sintéticos allowlisted
  Agent-->>Temporal: assertions e artefatos
  Temporal->>Telemetry: consultas limitadas de métricas, logs, traces e SLO
  Temporal-->>API: PASS, FAIL ou INCONCLUSIVE com evidência
  API-->>CI: decisão determinística
```

## Promoção para produção

```mermaid
flowchart LR
  Change[Branch de mudança] --> PR[Pull Request]
  PR --> CI[Tests, lint, scan, SBOM e manifests]
  CI --> Review[CODEOWNERS e aprovação]
  Review --> Main[Merge em main protegida]
  Main --> Release[Imagem multiarch por digest e assinatura OIDC]
  Release --> GitOps[Atualização declarativa no repositório GitOps]
  GitOps --> Argo[Argo CD reconcilia]
  Argo --> Cluster[Cluster alvo]
  Cluster --> Runtime[Readiness, SLO, restore e rollback]
```

A CI valida e publica; ela não deve aplicar produção de forma imperativa. O
cluster só é aprovado depois que políticas, IdP, backends, dados, restore e
rollback forem comprovados no alvo real.
