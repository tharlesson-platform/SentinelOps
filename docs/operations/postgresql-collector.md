# Collector PostgreSQL read-only

`apps/postgresexporter` publica somente estatísticas agregadas de conexão,
atividade, espera, locks não concedidos, limite de conexões e deadlocks. A
consulta é fixa; ela não lê tabelas de negócio, SQL em execução, payloads,
usuários, endereços de cliente ou segredos.

## Credencial e transporte

O DBA deve criar uma identidade exclusiva de monitoramento, sem
`SUPERUSER`, `CREATEDB`, `CREATEROLE`, herança de roles de aplicação ou grants
em tabelas de negócio. Ela precisa apenas de conectividade e do papel
predefinido `pg_monitor` — valide a política efetiva antes do rollout. O
collector não cria usuário, role ou grant por conta própria.

Armazene um DSN com `sslmode=verify-full` em arquivo `0600`, incluindo CA e
server name quando necessários. O processo rejeita DSN sem TLS validado,
arquivo legível por grupo/outros, conteúdo vazio, bind não-loopback e intervalo
fora de 15 s a 10 min.

Defina também `POSTGRES_MONITOR_ASSET_ID`, `POSTGRES_MONITOR_TEAM` e
`POSTGRES_MONITOR_ENVIRONMENT` como identificadores estáveis. Eles são as
únicas labels emitidas pelo exporter e permitem que conexão alta, lock, deadlock
ou collector vencido sejam roteados ao owner correto sem cardinalidade aberta.

```sh
docker build --build-arg APP=postgresexporter \
  -t sentinelops-postgres-exporter:0.1.0-local \
  -f Dockerfile.app .

POSTGRES_MONITOR_DSN_FILE=/etc/sentinelops/postgres-monitor.dsn \
POSTGRES_EXPORTER_ADDRESS=127.0.0.1:9187 \
POSTGRES_SCRAPE_INTERVAL=30s \
./postgresexporter
```

O Compose possui o profile desativado `database-monitor`, incluindo o exporter
na cadeia de build e lock local. Somente após montar o DSN real e verificado,
suba o profile explicitamente:

```sh
SENTINEL_POSTGRES_MONITOR_DSN_FILE=/caminho/protegido/postgres-monitor.dsn \
docker compose --env-file .env -f deploy/compose/docker-compose.yml \
  --profile database-monitor up -d postgres-exporter
```

Em produção, substitua a tag local por imagem assinada e digerida, entregue o
arquivo de DSN por secret manager/mount `0600` e exponha `/metrics` apenas a um
Prometheus/Alloy privado. O endpoint `/-/ready` só retorna 200 após coleta
bem-sucedida recente; falha de TLS, acesso ou banco torna `scrape_success=0` e
readiness 503. Os logs registram apenas uma classe sanitizada de erro.

O Prometheus local já possui scrape privado e regras para stale/falha do
collector, saturação acima de 85%, locks persistentes e deadlocks. Elas não usam
`absent()` porque o profile é opcional: nenhum alerta é gerado apenas por o
collector estar deliberadamente desligado antes da homologação.

## Rollout e rollback

Comece por um banco de homologação. Compare conexões, locks e deadlocks com
`pg_stat_*` sob supervisão do DBA; induza uma contenção controlada sem query de
negócio sensível e valide o alerta/owner. Para rollback, pare o exporter,
remova o scrape privado e revogue a role de monitoramento; não modifique dados,
schema ou processos do banco.

MySQL, SQL Server, Redis e filas ainda não são cobertos por este collector.
