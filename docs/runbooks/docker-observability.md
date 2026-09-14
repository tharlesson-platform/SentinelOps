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

## Rollback

Remova `beyla` de `COMPOSE_PROFILES` (ou remova o drop-in), execute `systemctl daemon-reload` e reinicie
`sentinelops-collector.service`. Os contêineres de negócio não são alterados.
As séries históricas e os logs já enviados permanecem nas retenções centrais.
