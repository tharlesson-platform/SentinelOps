# Plano de entrega para produção

Backlog vivo iniciado em 2026-09-14. Um item só muda de `[ ]` para `[x]`
quando o critério de aceite e os testes indicados possuem evidência. `PASS`
local não fecha homologação de IdP, cloud, cluster, collector, rede, CI remoto ou
DR. A prioridade desta iteração é Host, Docker, Logs, Metrics, APM, Traces,
Profiles e correlação de contexto.

## Regras de execução

- Preservar a arquitetura Go/React/PostgreSQL/Temporal e o Data Plane atual;
  mudança estrutural exige ADR, benchmark e rollback.
- Prometheus/Mimir, Loki, Tempo e Pyroscope nunca são consultados diretamente
  pelo navegador. O Control Plane reaplica autenticação, RBAC, tenant, quotas,
  timeout, limites e allowlist de query.
- Campo sem evidência é `null`/`Sem dados`; fonte quebrada é
  `Telemetry unavailable`; capacidade ausente é `Metric unsupported`.
- Ao final de cada fase: `git diff --check`, build, lint, unit, integration e E2E
  aplicáveis; resultado e timestamp entram em “Checkpoints”.

## P0 — Bloqueadores de execução e segurança

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [x] P0-01 Isolar teste de config | Impede falso negativo no gate Go sem afrouxar autenticação | `internal/config/config_test.go` | Suite `internal/config` verde com race | `go test -race ./internal/config` PASS 2026-09-14 | BAIXO |
| [x] P0-02 Upgrade seguro do `.env` | Instalações existentes voltam a renderizar Compose sem perder secrets | `scripts/bootstrap.sh`, `.env.example` | Defaults ausentes anexados; valores existentes intactos; modo 0600 | bootstrap + `docker compose config --quiet` PASS 2026-09-14 | MÉDIO |
| [x] P0-03 Registry MinIO válido | Sem isso o stack não inicia | `deploy/compose/docker-compose.yml`, `docs/architecture/component-versions.md` | Mesmo tag/digest multiarch baixa do registry oficial e containers iniciam | `imagetools inspect` + stack saudável PASS 2026-09-14 | MÉDIO |
| [x] P0-04 Cliente de query tenant-aware | Habilita UI nativa sem expor backends nem queries arbitrárias | `internal/telemetryquery`, `internal/config`, `internal/httpapi` | Headers tenant corretos, limite 8 MiB, timeout, retry transitório e parsing validado | packages focados com race PASS 2026-09-14 | ALTO |
| [ ] P0-05 Gateway de query para API | Evita acesso direto da API a backends produtivos | Caddy, Helm deployments/NetworkPolicy/values | SAN `control-plane-api`, secret próprio, egress exclusivo; negação para cert inválido | lint/template/kubeconform/Caddy PASS; teste mTLS em cluster pendente | ALTO |
| [x] P0-06 Gate completo limpo | Garante que a árvore consolidada não carrega regressão | todo repo | `make harness-check` PASS após todas as mudanças | unit/race/web/collectors PASS 2026-09-14 | ALTO |
| [ ] P0-07 Isolar Data Plane privado | Prometheus/Loki/Web não podem contornar autenticação, RBAC e tenancy | Compose do piloto, firewall e runbook | 9090/3100 indisponíveis fora do host; Web somente pelo acesso aprovado; gateway continua funcional | scan TCP externo + consultas positivas pelo Control Plane e negativas diretas | CRÍTICO |

## P1 — Observabilidade essencial

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P1-01 Operations Overview real | Responde hosts/containers/services sem números inventados | API `/observability/overview`, `App.tsx` | Contagens vivas ou estado explícito de ausência/falha | API contra Prometheus local + E2E | MÉDIO |
| [ ] P1-02 Hosts global nativo | Lista host, OS, ambiente, CPU, memória, disco e health | `observability.go`, `Observability.tsx` | Todos os hosts `linux-node` aparecem; filtros e null states corretos | unit join + collector real | ALTO |
| [ ] P1-03 Host Details completo | Permite investigar CPU modes/load/mem/swap/filesystem/I/O/rede | mesmos arquivos; recording rules | Séries 15m/1h/6h/24h; ausência por métrica não derruba página | range API + UI E2E | ALTO |
| [ ] P1-04 Processos e TCP | Fecha top process, conexões, retransmissions e OOM sem vazar command line | collector/process exporter, query builders | Capacidade opt-in, redaction e overhead medido | host Linux autorizado | ALTO |
| [ ] P1-05 Docker global nativo | Lista containers vivos com recursos e host | cAdvisor, API/UI | identidade/CPU/mem/limit/restarts válidos; stale distinto de stopped | cAdvisor local + E2E | ALTO |
| [ ] P1-06 Container Details | CPU/throttle/mem/fs/network e lifecycle correlacionados | cAdvisor + inventário Docker | drill-down preserva host/container/service; health/exit/OOM com fonte real | API/UI + restart controlado | ALTO |
| [ ] P1-07 Logs por host/container/service | Investigação no Loki sem abrir Grafana | collector Docker, API/UI Logs | filtros produzem eventos corretos; label `container_id`; falha Loki é parcial | Loki local + collector real | ALTO |
| [ ] P1-08 Live Tail | Incidente em tempo real com cancelamento e backpressure | Loki tail, API SSE/WS, UI | conexão autenticada/tenant, limites, reconnect e cancelamento | E2E e teste de carga | ALTO |
| [ ] P1-09 Metrics Explorer | Usuário monta consultas sem PromQL; operador tem modo avançado separado | AST/query builder, RBAC, UI | selector/labels/aggregation/rate seguros e paginados | fuzz/unit + Prometheus integration | ALTO |
| [ ] P1-10 APM/RED | Lista services/endpoints com rate/errors/p50–p99 | OTel semantic conventions, API/UI | dados reais por service/route; labels normalizadas | demo instrumentada + app real | ALTO |
| [ ] P1-11 Traces e waterfall | Busca e detalhe Tempo, trace→logs e context preservation | Tempo API, UI waterfall | trace e spans reais; links logs/service/deployment | E2E correlacionado por trace_id | ALTO |
| [ ] P1-12 Profiles nativo | Flamegraph Pyroscope sem wrapper Grafana | gateway route, parser, renderer | CPU/memory/allocation/profile real ou unsupported explícito | profile demo + app real | ALTO |
| [ ] P1-13 Recording rules | Reduz custo de RED/SLO/host/container frequentes | `deploy/prometheus/rules.yml` | promtool verde e cardinalidade estimada | promtool + query benchmark | MÉDIO |
| [ ] P1-14 Frota Docker Azure | Cobre `tqi-platform`, `easy-vm` e `gitlab-vm` sem reiniciar workloads | collectors, PKI, gateway e `azure-docker-rollout-2026-09-14.md` | três certificados exclusivos, collectors ready e sinais frescos por host | 2/3 ativos; GitLab aguarda emissão da CA; TQI exige tratamento de disco/retenção | ALTO |

## P2 — Dashboards e experiência SRE

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P2-01 Navegação contextual | Mantém org/env/host/container/service em Metrics/Logs/Traces/Profiles | estado/URL do frontend | deep link compartilhável e contexto preservado no refresh | Playwright | MÉDIO |
| [ ] P2-02 Investigate + timeline | Uma visão integra deploy, alert, synthetic, infra, log e trace | event model/API/UI | ordenação UTC, provenance e drill-down | fixture + integração real | ALTO |
| [ ] P2-03 Custom dashboards | Layout persistente com widgets e variáveis | migrations/store/API/UI | CRUD, versionamento, RBAC, drag/resize e share params | unit/API/Playwright | ALTO |
| [ ] P2-04 Default dashboards | Entrega templates nativos para infra/K8s/ECS/APM/SLO/etc. | editor e query builders | cada painel declara fonte e no-data state | snapshot + backend integration | MÉDIO |
| [ ] P2-05 Dark/light/responsive | Operação legível em NOC, notebook e mobile | CSS/design tokens | contraste, overflow e estados nos dois temas | visual regression | MÉDIO |
| [ ] P2-06 i18n pt-BR/en-US | Remove strings operacionais hardcoded | catálogos i18next | 100% das mensagens críticas traduzíveis | lint de chaves + Playwright | BAIXO |

## P3 — Delivery Assurance

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P3-01 Release UI/API completa | Releases, validations e evidências deixam de ser tela vazia | store/routes/UI | list/detail/watch/cancel vinculados a SHA/digest | API + Temporal E2E | ALTO |
| [ ] P3-02 Baseline e stabilization | Comparação antes/depois estatisticamente válida | workflow/policies | janela, sample minimum, freshness e low traffic tratados | deterministic workflow tests | ALTO |
| [ ] P3-03 Deployment markers | Marca versões em charts e timeline | release events + UI | marker com version/SHA/digest e link de validação | E2E | MÉDIO |
| [ ] P3-04 Adapters CI/CD | GitHub/GitLab/Jenkins/Azure/CodePipeline assinados | webhook/outbox/examples | replay/idempotência, provenance e gate real por provider | ambientes autorizados | ALTO |
| [ ] P3-05 Correlation Engine | Classifica correlação sem afirmar causalidade | multi-signal event model | evidência e contraprova para highly/probable/possible/inconclusive | cenário controlado | ALTO |

## P4 — Synthetic Monitoring

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P4-01 HTTP/API completo | Status/header/body/JSONPath/latency com SSRF fail-closed | scheduler/agent | DNS pinning, redirects e allowlist hostname@CIDR provados | unit/race/integration | ALTO |
| [ ] P4-02 TCP/DNS/ICMP/TLS | Cobertura de rede e expiração TLS por localidade | agent runners | capacidades reais, timeout e evidence artifact | targets autorizados | ALTO |
| [ ] P4-03 Browser/k6 runners | Executa Playwright/k6 distribuído e isolado | runner queue, object storage | screenshot/trace/HAR/video/JUnit/JSON visíveis e retidos | isolated E2E | ALTO |
| [ ] P4-04 Test Studio importers | cURL/OpenAPI/Postman/HAR/YAML/k6/Playwright | parsers/UI | preview sanitizado, validation e versioning | corpus/fuzz/Playwright | MÉDIO |

## P5 — SLO e alerting

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P5-01 CRUD SLO/error budget | Define availability/latency/error/custom PromQL com ownership | migrations/store/API/UI | compliance, budget e burn rate reais por janela | Prometheus integration | ALTO |
| [ ] P5-02 Multi-window burn rate | Reduz falso positivo e detecta consumo rápido/lento | rules/Alertmanager | pares de janelas e severidade documentados | promtool + replay histórico | ALTO |
| [ ] P5-03 Alertmanager nativo | Lista firing/pending/resolved e abre contexto | client/API/UI | falha Alertmanager não derruba Investigate | integration + UI | ALTO |
| [ ] P5-04 Incident lifecycle | ACK/resolve/escalation/notification auditáveis | outbox/dispatcher/UI | retry/idempotência, four-eyes quando aplicável | PostgreSQL integration | ALTO |

## P6 — Segurança e RBAC

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P6-01 OIDC/Entra/Keycloak | Identidade corporativa, MFA delegado e claims estáveis | IdP + Helm/config | issuer/audience/scope/org/role e logout homologados | IdP real | CRÍTICO |
| [ ] P6-02 Matriz RBAC total | Backend autoriza todas as rotas e objetos | auth/store/API | deny-by-default e tests por role/route | table-driven + API | CRÍTICO |
| [ ] P6-03 Auditoria total | Login/logout/dashboard/SLO/test/override/RBAC/token | events/migrations | evento imutável com actor/requestId/IP e sem secret | integration | ALTO |
| [ ] P6-04 Security gates | SAST/SCA/SBOM/secret/image scan/signature | CI/release | artefato assinado por digest e policy de promoção | CI remoto no SHA | CRÍTICO |
| [ ] P6-05 Threat model/pen test | Valida SSRF, tenancy, CSRF, replay, rate limits e gateways | docs/tests/ambiente | nenhum escape cross-tenant ou acesso backend direto | teste independente | CRÍTICO |

## P7 — Escalabilidade e capacidade

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P7-01 HA Control Plane | API/worker/web/migrations suportam falha e rollout | Helm/PostgreSQL/Temporal | PDB/topology/HPA/failover sem perda/duplicação | chaos controlado | ALTO |
| [ ] P7-02 Data Plane distribuído | Mimir/Loki/Tempo/Pyroscope com object storage e tenancy | stack externo | HA, compaction, retention e restore por backend | staging representativo | CRÍTICO |
| [ ] P7-03 Cardinalidade/ingestão | Top metrics/labels, drops, growth e quotas | self-metrics/query API | labels perigosas detectadas e budgets alertáveis | carga sintética | ALTO |
| [ ] P7-04 Capacity Agent | 7/30/90d, MAD/EWMA/regression com incerteza | histórico suficiente | forecast reproduzível e claramente estimado | backtest | MÉDIO |
| [ ] P7-05 Performance budget | API/dashboard/log/trace/ingestão/concurrency | k6/Playwright | SLOs de desempenho e limites documentados | carga controlada | ALTO |

## P8 — Production hardening

| Tarefa | Descrição e impacto | Dependências / arquivos | Critério de aceite | Testes | Risco |
|---|---|---|---|---|---|
| [ ] P8-01 Backup/restore | PostgreSQL, object storage e definições recuperáveis | backupcrypt/scripts/keys | restore íntegro dentro de RPO/RTO | restore isolado | CRÍTICO |
| [ ] P8-02 DR | Recuperação regional/cluster e dependências | IaC/runbooks | exercício com timestamps, decisão e rollback | game day autorizado | CRÍTICO |
| [ ] P8-03 Upgrade/rollback | N-1→N e rollback sem corrupção | migrations/images/GitOps | runbook executado no staging | rehearsal | ALTO |
| [ ] P8-04 Runbooks mínimos | Backends, disk, cardinality, agents, synthetic e validation | `docs/runbooks/` | cada runbook tem sinais, diagnóstico, mitigação, rollback e escalonamento | tabletop | MÉDIO |
| [ ] P8-05 Licenças/SBOM | Compatibilidade Apache-2.0 e inventário transitivo | dependencies/CI | relatório sem incompatibilidade não aceita | scanner + revisão legal | ALTO |
| [ ] P8-06 Evidência por ambiente | DEV/STG/PRD não compartilham conclusão | harness/artifacts/docs | provenance, target, SHA, horário e resultado por gate | harness release/production | CRÍTICO |
| [ ] P8-07 Revisão SRE independente | Último gate antes de merge/deploy | diff, evidências e alvo | nenhum risco CRÍTICO/ALTO aberto | T-800 review | CRÍTICO |

## Checkpoints

| Fase | Build | Lint | Unit/race | Integration | E2E | Resultado |
|---|---|---|---|---|---|---|
| Auditoria inicial | web PASS; imagens próprias PASS | não executado | focados PASS | não executado | não executado | Em andamento; produção `REJECTED` |
| Fundação P0/P1 local | Go/web/imagens PASS | Compose, Promtool, Alloy, Helm, kubeconform e Caddy PASS | `make harness-check` PASS | PostgreSQL Compose + pipeline multi-sinal + HA PASS | Playwright 1/1; k6 150 iterações e 450 checks PASS | Laboratório local aprovado; produção externa `REJECTED` |
| Rollout Docker Azure | imagens e arquivos conferidos por hash | units collectors e logrotate verificadas; rotação Easy/GitLab PASS | preflight/runtime GitLab PASS | gateway TCP PASS em 3 hosts Docker | freshness backend PASS em 2/3 | GitLab aguarda identidade mTLS; TQI aguarda retenção segura; `REJECTED` |

## Critério de encerramento

O `final-report.md` só será criado como relatório final quando as 36 jornadas do
pedido tiverem evidência suficiente. Enquanto houver qualquer P0, risco
CRÍTICO/ALTO ou gate externo obrigatório aberto, o relatório e a UI devem dizer
claramente `REJECTED`/`INCONCLUSIVE`; não existe aprovação implícita.
