# Observabilidade de contêineres Docker

## Escopo

O perfil `containers` coleta logs JSON em `stdout` e `stderr`; o perfil
`cadvisor` coleta CPU, memória, rede, filesystem e ciclo de vida por
contêiner; e o perfil `beyla` produz APM HTTP/gRPC (traces, RED, processos,
runtime JVM/Go e grafo de serviço) sem alterar imagens ou comandos das apps.
Todos são opt-in por host Docker.

O eBPF cobre protocolos HTTP e gRPC das portas declaradas no
`beyla.yml`; não substitui instrumentação nativa para protocolos proprietários,
jobs sem porta de escuta, RUM de navegador ou profiling contínuo. Para esses
casos, aplique o onboarding OpenTelemetry específico da aplicação.

O leitor de logs não recebe o socket Docker e monta apenas
`/var/lib/docker/containers` como leitura. O cAdvisor precisa de uma exceção
privilegiada e do socket Docker somente leitura para inspecionar métricas de
runtime. Beyla também é privilegiado e compartilha PID/rede do host para
anexar programas eBPF: habilite-o apenas após validar BTF, tracefs e o modo de
lockdown do kernel.

## Habilitar

Instale o drop-in `sentinelops-collector-docker-observability.conf` em
`/etc/systemd/system/sentinelops-collector.service.d/`, execute
`systemctl daemon-reload` e reinicie somente `sentinelops-collector.service`.
Isso não reinicia os contêineres de negócio.

## Verificar

1. No host, confirme `container-logs` e `container-metrics` em execução no
   Compose do coletor, além de `beyla` quando APM eBPF estiver aprovado.
2. No Prometheus, consulte `count by (instance) (container_last_seen{name!=""})`.
3. No Loki, consulte `{job="docker-container",host_name="<host>"}`.
4. No Prometheus, consulte `count by (service_name) (http_server_request_duration_seconds_count)`.
5. No Tempo, filtre traces pelo serviço descoberto e confirme que a rota é normalizada.
6. No Grafana, abra **Docker Containers** e **APM - Hosts TQI** e filtre o host.

## Filtros das dashboards

As dashboards gerenciadas iniciam em `All`, mas todos os filtros são
multisseleção e alteram efetivamente as consultas. Selecione primeiro os
filtros da esquerda, pois os seguintes são encadeados:

- APM: **Aplicação → Rota ativa → Host → Aplicação/container**. A lista de
  rotas é limitada às 200 rotas com tráfego mais recente para evitar um menu
  inutilizável quando scanners geram milhares de caminhos diferentes.
- Logs: **Aplicação OTel → Ambiente → Host → Aplicação/container → Stream**,
  com busca por expressão regular no conteúdo. O nome amigável do container é
  resolvido pelas métricas do cAdvisor e convertido internamente em um ID
  oculto, usado para filtrar a label `filename` do Loki.
- Linux/Docker: **Ambiente → Host → Aplicação/container**; Linux acrescenta
  filesystem, interface de rede e busca nos logs.
- Sintéticos: **Grupo de sondas → Alvo**.
- VMware: **Endpoint → Máquina virtual → Datastore**.
- Alertas e self-monitoring: **Job → Instância → Severidade → Alerta**, além
  dos filtros de host/container para os painéis de logs.

O filtro por nome de container não depende da exposição do socket Docker no
leitor de logs: ele correlaciona o `id` já exportado pelo cAdvisor com o caminho
`filename` dos logs JSON montados em modo somente leitura. A label
`container_id` produzida por `config-docker-logs.alloy` continua disponível
para investigação, mas não é requisito para o dropdown funcionar.

Antes de publicar mudanças, execute:

```bash
./scripts/render-production-dashboards.sh
python3 scripts/check-dashboard-filters.py
```

## Rollback

Remova `beyla` de `COMPOSE_PROFILES` (ou remova o drop-in), execute `systemctl daemon-reload` e reinicie
`sentinelops-collector.service`. Os contêineres de negócio não são alterados.
As séries históricas e os logs já enviados permanecem nas retenções centrais.
