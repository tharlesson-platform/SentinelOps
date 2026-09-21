# Cobertura nativa de observabilidade — 21/09/2026

## Estado desta preparação

Implementação local preparada; a publicação e a equivalência candidato/fonte ainda dependem da validação no servidor. Este documento registra a auditoria das fontes, sem declarar conclusão do deploy.

## Universo observado

- 37 dashboards provisionados, 188 painéis e 193 alvos de consulta; definições remotas conferidas com os JSONs versionados.
- 885 nomes de métricas na janela do inventário, 377 famílias de metadata e 15 jobs. O catálogo pesquisável consulta uma métrica por vez; não foram disparadas 885 consultas simultâneas.
- 5 alvos de scrape locais e 22 regras. Alloy, Prometheus e API respondem; PostgreSQL e VMware têm erro de coleta na observação. Isso não comprova a saúde dos sistemas monitorados.
- Prometheus, Loki, Tempo e Pyroscope configurados. Pyroscope retornou 11 tipos de perfil e um serviço; visualização nativa implementada.

## Auditoria de consultas nas fontes

Janela UTC: 2026-09-21T11:20:15+00:00 até 2026-09-21T12:20:15+00:00. Passo: 15 segundos. Foram executadas 139 consultas únicas, sequencialmente, para os 193 alvos. Resultado: 157 com amostras, 36 sem dados e nenhum erro ao término da auditoria. A consulta de cardinalidade usa a métrica `up` como escolha explícita. Presença de amostras não equivale à saúde da aplicação.

A matriz abaixo registra cada painel configurado. “Informativo” identifica conteúdo sem consulta. Os números são por alvo, não por série ou evento. A equivalência com a API candidata será registrada separadamente após a execução.

| Dashboard | Painel | Tipo | Fonte | Com amostras | Sem dados | Erros | Unidade |
|---|---|---|---|---:|---:|---:|---|
| sentinel-api-test-results | 1 — Sonda real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-api-test-results | 2 — Onboarding de monitoramento sintético | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-apm | 1 — Aplicações selecionadas com métricas | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-apm | 2 — Requisições por segundo | stat | prometheus | 1 | 0 | 0 | reqps |
| sentinel-apm | 3 — Latência p95 | stat | prometheus | 1 | 0 | 0 | s |
| sentinel-apm | 4 — Logs da aplicação nos últimos 5 minutos | stat | loki | 1 | 0 | 0 | short |
| sentinel-apm | 5 — Erros HTTP 5xx por segundo | stat | prometheus | 1 | 0 | 0 | reqps |
| sentinel-apm | 6 — SLO de disponibilidade | stat | prometheus | 1 | 0 | 0 | percentunit |
| sentinel-apm | 7 — RED - volume por serviço e rota | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-apm | 8 — RED - latência p50/p95/p99 | timeseries | prometheus | 3 | 0 | 0 | s |
| sentinel-apm | 9 — Logs da aplicação selecionada | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-apm | 10 — Traces da aplicação selecionada | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-apm | 11 — Profiling: agente real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-aws-overview | 1 — Collector AWS Overview | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-aws-overview | 2 — Integração pendente | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-agent-fleet | 1 — Collectors Linux disponíveis | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-agent-fleet | 2 — Estado dos collectors | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-agent-fleet | 3 — Idade do último scrape | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-agent-fleet | 4 — Logs dos collectors | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-agent-fleet | 5 — Containers nomeados cobertos | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-alerts | 1 — Alertas firing no filtro | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-alerts | 2 — Alertas por severidade | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-alerts | 3 — Top alertas ativos | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-alerts | 4 — Logs de erro para correlação | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-alerts | 5 — Alertas críticos | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-application-overview | 1 — Aplicações selecionadas com métricas | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-application-overview | 2 — Requisições por segundo | stat | prometheus | 1 | 0 | 0 | reqps |
| sentinel-application-overview | 3 — Latência p95 | stat | prometheus | 1 | 0 | 0 | s |
| sentinel-application-overview | 4 — Logs da aplicação nos últimos 5 minutos | stat | loki | 1 | 0 | 0 | short |
| sentinel-application-overview | 5 — Erros HTTP 5xx por segundo | stat | prometheus | 1 | 0 | 0 | reqps |
| sentinel-application-overview | 6 — SLO de disponibilidade | stat | prometheus | 1 | 0 | 0 | percentunit |
| sentinel-application-overview | 7 — RED - volume por serviço e rota | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-application-overview | 8 — RED - latência p50/p95/p99 | timeseries | prometheus | 3 | 0 | 0 | s |
| sentinel-application-overview | 9 — Logs da aplicação selecionada | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-application-overview | 10 — Traces da aplicação selecionada | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-application-overview | 11 — Profiling: agente real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-azure-overview | 1 — Hosts Linux ativos | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-azure-overview | 2 — CPU por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-azure-overview | 3 — Memória utilizada por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-azure-overview | 4 — Logs reais de aplicações | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-azure-overview | 5 — Contêineres observados | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-browser-test-results | 1 — Sonda real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-browser-test-results | 2 — Onboarding de monitoramento sintético | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-capacity-forecast | 1 — Containers nomeados observados | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-capacity-forecast | 2 — CPU por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-capacity-forecast | 3 — Memória por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-capacity-forecast | 4 — Filesystem por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-capacity-forecast | 5 — Hosts disponíveis | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-cardinality-ingestion-cost | 1 — Targets saudáveis | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-cardinality-ingestion-cost | 2 — Séries ativas no filtro | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-cardinality-ingestion-cost | 3 — Ingestão de amostras | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-cardinality-ingestion-cost | 4 — Top 10 métricas por séries | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-cardinality-ingestion-cost | 5 — Amostras por scrape | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-continuous-profiling | 1 — Collector Continuous Profiling | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-continuous-profiling | 2 — Integração pendente | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-database-overview | 1 — Bancos em contêiner observados | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-database-overview | 2 — CPU dos bancos | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-database-overview | 3 — Memória dos bancos | timeseries | prometheus | 1 | 0 | 0 | bytes |
| sentinel-database-overview | 4 — Logs reais dos bancos | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-database-overview | 5 — Exporter PostgreSQL disponível | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-distributed-tracing | 1 — Serviços com tráfego rastreável | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-distributed-tracing | 2 — Top serviços por tráfego | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-distributed-tracing | 3 — Latência p95 dos serviços | timeseries | prometheus | 1 | 0 | 0 | s |
| sentinel-distributed-tracing | 4 — Logs da aplicação nos últimos 5 minutos | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-distributed-tracing | 5 — Traces da aplicação selecionada | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-docker-overview | 1 — Containers observados | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-docker-overview | 2 — CPU por container | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-docker-overview | 3 — Memória por container | timeseries | prometheus | 1 | 0 | 0 | bytes |
| sentinel-docker-overview | 4 — Rede por container | timeseries | prometheus | 2 | 0 | 0 | Bps |
| sentinel-docker-overview | 5 — I/O de filesystem | timeseries | prometheus | 1 | 0 | 0 | Bps |
| sentinel-ecs-overview | 1 — Collector ECS Overview | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-ecs-overview | 2 — Integração pendente | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-errors | 1 — Erros HTTP 5xx por segundo | stat | prometheus | 1 | 0 | 0 | reqps |
| sentinel-errors | 2 — Top 10 rotas por erros 5xx | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-errors | 3 — Taxa de erro por aplicação | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-errors | 4 — Logs de erro dos containers selecionados | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-errors | 5 — Traces com erro | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-executive-overview | 1 — Hosts disponíveis | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-executive-overview | 2 — Maior uso de CPU | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-executive-overview | 3 — Maior uso de memória | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-executive-overview | 4 — Alertas ativos | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-executive-overview | 5 — Aplicações com APM | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-frontend-observability | 1 — Frontends observados em Docker | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-frontend-observability | 2 — CPU dos frontends | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-frontend-observability | 3 — Memória dos frontends | timeseries | prometheus | 1 | 0 | 0 | bytes |
| sentinel-frontend-observability | 4 — Logs reais de frontend | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-frontend-observability | 5 — RUM/Web Vitals instrumentado | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-incident-timeline | 1 — Sonda real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-incident-timeline | 2 — Onboarding de monitoramento sintético | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-kubernetes-overview | 1 — Collector Kubernetes Overview | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-kubernetes-overview | 2 — Integração pendente | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-latency | 1 — Latência p95 | stat | prometheus | 1 | 0 | 0 | s |
| sentinel-latency | 2 — Top 10 rotas mais lentas | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-latency | 3 — Tendência de latência | timeseries | prometheus | 3 | 0 | 0 | s |
| sentinel-latency | 4 — Logs da aplicação nos últimos 5 minutos | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-latency | 5 — Traces acima de 1 segundo | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-linux-overview | 1 — Hosts ativos | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-linux-overview | 2 — Uptime | stat | prometheus | 1 | 0 | 0 | s |
| sentinel-linux-overview | 3 — CPU utilizada | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-linux-overview | 4 — Memória utilizada | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-linux-overview | 5 — Filesystem utilizado | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-linux-overview | 6 — Processos | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-linux-overview | 7 — CPU por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-linux-overview | 8 — Memória por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-linux-overview | 9 — Rede RX/TX | timeseries | prometheus | 2 | 0 | 0 | Bps |
| sentinel-linux-overview | 10 — Filesystem por montagem | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-linux-overview | 11 — Inventário Linux | table | prometheus | 1 | 0 | 0 | não informada |
| sentinel-linux-overview | 12 — Logs do sistema | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-logs | 1 — Eventos no período | stat | loki | 1 | 1 | 0 | short |
| sentinel-logs | 2 — Volume de logs por aplicação/container | timeseries | loki | 1 | 1 | 0 | short |
| sentinel-logs | 3 — Possíveis erros por intervalo (regex) | timeseries | loki | 1 | 1 | 0 | short |
| sentinel-logs | 4 — Logs filtrados (máximo 200 linhas) | logs | loki | 1 | 1 | 0 | não informada |
| sentinel-logs | 5 — Possíveis erros no período (regex) | stat | loki | 1 | 1 | 0 | short |
| sentinel-messaging-overview | 1 — Collector Messaging Overview | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-messaging-overview | 2 — Integração pendente | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-noc-overview | 1 — Alertas ativos | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-noc-overview | 2 — Alertas críticos | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-noc-overview | 3 — Hosts sem coleta (esperado: 3) | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-noc-overview | 4 — Maior uso de memória | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-noc-overview | 5 — Maior uso de filesystem | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-noc-overview | 6 — Taxa HTTP 5xx | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-noc-overview | 7 — Fila de atuação — alertas firing | table | prometheus | 1 | 0 | 0 | não informada |
| sentinel-noc-overview | 8 — Memória por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-noc-overview | 9 — Top aplicações por erro 5xx | table | prometheus | 0 | 1 | 0 | não informada |
| sentinel-noc-overview | 10 — Volume de erros recentes por host | timeseries | loki | 1 | 0 | 0 | short |
| sentinel-release-comparison | 1 — Sonda real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-release-comparison | 2 — Onboarding de monitoramento sintético | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-release-validation | 1 — Sonda real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-release-validation | 2 — Onboarding de monitoramento sintético | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-slo-error-budget | 1 — Disponibilidade HTTP (30 min) | stat | prometheus | 1 | 0 | 0 | percentunit |
| sentinel-slo-error-budget | 2 — Aplicações com menor disponibilidade | timeseries | prometheus | 1 | 0 | 0 | percentunit |
| sentinel-slo-error-budget | 3 — Taxa de erro consumindo o SLO | timeseries | prometheus | 1 | 0 | 0 | percentunit |
| sentinel-slo-error-budget | 4 — Logs da aplicação nos últimos 5 minutos | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-slo-error-budget | 5 — Traces da aplicação selecionada | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-sentinelops-self-monitoring | 1 — Alvos Prometheus saudáveis | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-sentinelops-self-monitoring | 2 — Séries ativas no TSDB | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-sentinelops-self-monitoring | 3 — Amostras ingeridas por segundo | timeseries | prometheus | 1 | 0 | 0 | reqps |
| sentinel-sentinelops-self-monitoring | 4 — Logs filtrados da aplicação / container | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-sentinelops-self-monitoring | 5 — Alertas em firing | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-service-graph | 1 — Dependências observadas | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-service-graph | 2 — Top dependências por volume | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-service-graph | 3 — Latência p95 entre serviços | timeseries | prometheus | 1 | 0 | 0 | s |
| sentinel-service-graph | 4 — Logs da aplicação nos últimos 5 minutos | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-service-graph | 5 — Traces da aplicação selecionada | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-service-health | 1 — Aplicações com tráfego | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-service-health | 2 — Top aplicações por erro 5xx | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-service-health | 3 — Latência p95 por aplicação | timeseries | prometheus | 1 | 0 | 0 | s |
| sentinel-service-health | 4 — Logs da aplicação nos últimos 5 minutos | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-service-health | 5 — Traces da aplicação selecionada | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-synthetic-monitoring | 1 — Sonda real disponível | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-synthetic-monitoring | 2 — Onboarding de monitoramento sintético | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-tqi-hosts-apm | 1 — Cobertura centralizada | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-tqi-hosts-apm | 2 — Hosts com coleta | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-tqi-hosts-apm | 3 — CPU utilizada | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-tqi-hosts-apm | 4 — Memória utilizada | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-tqi-hosts-apm | 5 — Maior uso de filesystem | stat | prometheus | 1 | 0 | 0 | percent |
| sentinel-tqi-hosts-apm | 6 — Logs com erro (15 min) | stat | loki | 1 | 0 | 0 | short |
| sentinel-tqi-hosts-apm | 7 — Serviços APM com tráfego | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-tqi-hosts-apm | 8 — CPU por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-tqi-hosts-apm | 9 — Memória por host | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-tqi-hosts-apm | 10 — Rede — recebimento e transmissão | timeseries | prometheus | 2 | 0 | 0 | Bps |
| sentinel-tqi-hosts-apm | 11 — APM RED — requisições por segundo | timeseries | prometheus | 1 | 0 | 0 | reqps |
| sentinel-tqi-hosts-apm | 12 — APM RED — latência p95 | timeseries | prometheus | 1 | 0 | 0 | s |
| sentinel-tqi-hosts-apm | 13 — APM RED — taxa de erros 5xx | timeseries | prometheus | 1 | 0 | 0 | percent |
| sentinel-tqi-hosts-apm | 14 — APM — traces por aplicação e host | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-tqi-hosts-apm | 15 — Logs das aplicações e do sistema | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-throughput | 1 — Requisições por segundo | stat | prometheus | 1 | 0 | 0 | reqps |
| sentinel-throughput | 2 — Top 10 rotas por volume | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-throughput | 3 — Throughput por aplicação | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-throughput | 4 — Logs da aplicação nos últimos 5 minutos | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-throughput | 5 — Traces da aplicação selecionada | traces | tempo | 1 | 0 | 0 | não informada |
| sentinel-vmware-overview | 1 — Collector VMware Overview | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-vmware-overview | 2 — Integração pendente | text | Informativo | 0 | 0 | 0 | não informada |
| sentinel-vmware-vm-performance | 1 — Coleta de desempenho VMware | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-vmware-vm-performance | 2 — VMs ligadas | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-vmware-vm-performance | 3 — VMware Tools em execução | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-vmware-vm-performance | 4 — Datastore livre | stat | prometheus | 0 | 1 | 0 | short |
| sentinel-vmware-vm-performance | 5 — Rede das VMs | timeseries | prometheus | 0 | 2 | 0 | Bps |
| sentinel-vmware-vm-performance | 6 — Throughput de disco das VMs | timeseries | prometheus | 0 | 2 | 0 | Bps |
| sentinel-vmware-vm-performance | 7 — IOPS de disco das VMs | timeseries | prometheus | 0 | 2 | 0 | short |
| sentinel-vmware-vm-performance | 8 — Latência de disco das VMs | timeseries | prometheus | 0 | 2 | 0 | s |
| sentinel-vmware-vm-performance | 9 — CPU das VMs | timeseries | prometheus | 0 | 1 | 0 | short |
| sentinel-vmware-vm-performance | 10 — Memória das VMs | timeseries | prometheus | 0 | 1 | 0 | bytes |
| sentinel-web-vitals | 1 — Frontends observados em Docker | stat | prometheus | 1 | 0 | 0 | short |
| sentinel-web-vitals | 2 — CPU dos frontends | timeseries | prometheus | 1 | 0 | 0 | short |
| sentinel-web-vitals | 3 — Memória dos frontends | timeseries | prometheus | 1 | 0 | 0 | bytes |
| sentinel-web-vitals | 4 — Logs reais de frontend | logs | loki | 1 | 0 | 0 | não informada |
| sentinel-web-vitals | 5 — RUM/Web Vitals instrumentado | stat | prometheus | 0 | 1 | 0 | short |

## Semântica e limites

As consultas usam as definições provisionadas, defaults, unidades, legendas, labels e timestamps. Os 81 stats usam `lastNotNull`; três tabelas não possuem transformações. `percent` representa 0–100 e `percentunit` representa fração 0–1. Valores indefinidos não se transformam em zero. Dados sem unidade/tipo permanecem desconhecidos; metadata conflitante não habilita taxa de contador.

Valores digitados são literais escapados. “Todos” respeita `allValue`; sem `allValue`, a API resolve a lista completa da variável e aplica a regex provisionada. Falha, lista truncada ou expressão acima de 8 KiB exige seleção explícita, sem ampliar a consulta. IDs de exporters não são convertidos em `host_name`.

Consultas têm prazo compartilhado de 10 s, até duas por réplica sem fila, quota por chamada de fonte, resposta de até 8 MiB, 40 séries, 360 pontos, 200 logs ou 50 traces. A janela máxima é 24 h. Esses limites podem produzir resultado parcial; não representam a cardinalidade completa. Perfis agregam a janela, com `maxNodes=200`, podendo incluir nós agrupados da fonte. Valores inteiros e unidade original são preservados na tabela.

Descoberta é habilitada no modo local de uma organização. Em OIDC, permanece bloqueada até configurar `TELEMETRY_DISCOVERY_TENANT_AWARE=true` depois de comprovar isolamento em cada backend. Prometheus standalone não isola tenants pelo header. Alvos/regras globais continuam indisponíveis em OIDC. Gateway utiliza o listener de consulta, certificado exclusivo da API, métodos/caminhos fixos e organização única; não concede discovery ao worker.

## Validação local e procedimento operacional

20 testes unitários web aprovados. Pacotes Go de `apps/...`, `internal/...` e `dashboards` aprovados. A invocação `go test ./...` no checkout operacional também encontrou cópias antigas e incompletas em `artifacts/`; elas não fazem parte do código versionado.

Gateway: 11 casos mTLS aprovados, incluindo negação do worker, header duplicado/federado, falta de organização e método inválido. Helm lint/render e adaptação Caddy aprovados.

Drenagem ensaiada em ambiente Docker isolado: 91 requisições, zero falhas, requisição longa concluída e rollback provocado. Barreira de 3,03 s esperou a geração anterior sair. Quatro testes adicionais cobrem rollback após retorno ao DNS, falha na primeira aplicação do edge e timeout que preserva todas as APIs e candidata rejeitada que nunca entra no tráfego. O ensaio não é evidência de indisponibilidade zero em produção.

A release usa backup do bundle, runtime, lock de imagens e edge; admite nova API apenas após readiness, login e comparação com fontes. Retira a antiga somente após reload gracioso e término dos workers anteriores. O arquivo montado mantém seu inode, com hash conferido dentro do container. Nenhum volume ou serviço fora da API é removido. A fonte autoritativa fica em `release/source/`, ligada ao commit da imagem.

Referências: [API Prometheus](https://prometheus.io/docs/prometheus/latest/querying/api/), [contrato Pyroscope](https://github.com/grafana/pyroscope/blob/main/api/querier/v1/querier.proto), [formato flamegraph](https://github.com/grafana/pyroscope/blob/main/pkg/og/structs/flamebearer/flamebearer.go), [reload gracioso Nginx](https://nginx.org/en/docs/control.html).

## Revisão independente da preparação

APPROVED WITH CHANGES para commit, build e rollout controlado, condicionado à comparação obrigatória da candidata antes da admissão. A aprovação não constitui evidência de publicação nem de indisponibilidade zero. Falha de comparação aciona recuperação; timeout de drenagem mantém as réplicas vivas.
