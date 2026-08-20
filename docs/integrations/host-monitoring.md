# Onboarding de servidores Linux

O collector Linux usa Grafana Alloy com os componentes embutidos Unix Exporter
(baseado em Node Exporter) e cAdvisor opcional. O host inicia conexões de saída
para o SentinelOps; nenhuma porta de exporter é publicada na rede.

## Sinais coletados

- Unix Exporter: CPU, load, memória, swap, filesystem, inodes, disco, rede,
  TCP, processos, file descriptors, clock, uptime e kernel.
- Logs: arquivos `*.log` diretamente abaixo de `/var/log`, com descarte de
  linhas que aparentem conter credenciais.
- OTLP local: aplicações do host enviam métricas, logs e traces para
  `127.0.0.1:4317` ou `127.0.0.1:4318`.
- cAdvisor opcional: CPU, memória, rede, I/O, estado e restarts de containers.

O componente cAdvisor necessita acesso privilegiado ao kernel, socket Docker e
diretórios do runtime.
Habilite-o somente em hosts Docker aprovados e registre essa exceção de
segurança.

O mount raiz é somente leitura e captura os filesystems presentes no início do
collector. Se o host cria mounts dinamicamente depois do start, reinicie o
collector na janela aprovada para atualizar essa descoberta.

Se o collector for executado no mesmo host do stack central, altere as portas
locais para evitar conflito com o Alloy central:

```bash
./scripts/install-linux-collector.sh --phase all \
  --metrics-endpoint http://host.docker.internal:9090/api/v1/write \
  --logs-endpoint http://host.docker.internal:3100/loki/api/v1/push \
  --otlp-endpoint http://host.docker.internal:4318 \
  --admin-port 12346 --otlp-grpc-port 14317 --otlp-http-port 14318 \
  --allow-insecure
```

`host.docker.internal` é adicionado ao gateway do host pelo Compose. Não use
`127.0.0.1` como destino remoto: dentro do container ele aponta para o próprio
collector.

## Preparar o servidor central

Escolha um IP privado, por exemplo `10.20.30.40`, e refaça a fase de
configuração/deploy:

```bash
./scripts/install-linux-server.sh \
  --phase configure \
  --web-bind 10.20.30.40 \
  --ingest-bind 10.20.30.40
./scripts/install-linux-server.sh --phase deploy
./scripts/install-linux-server.sh --phase verify
```

No firewall, permita somente coletores autorizados para TCP `4317`, `4318`,
`9090` e `3100`. Prefira um gateway HTTPS autenticado; os receivers diretos do
perfil single-node não possuem autenticação.

## Instalar o collector

Copie o diretório do projeto ou empacote `deploy/agents/linux`, `scripts/lib` e
`scripts/install-linux-collector.sh` no host. Execute fase por fase:

```bash
./scripts/install-linux-collector.sh --phase preflight
./scripts/install-linux-collector.sh --phase runtime
./scripts/install-linux-collector.sh \
  --phase configure \
  --host-name srv-app-01 \
  --environment PRD \
  --team pagamentos \
  --location dc-sp-01 \
  --metrics-endpoint http://10.20.30.40:9090/api/v1/write \
  --logs-endpoint http://10.20.30.40:3100/loki/api/v1/push \
  --otlp-endpoint http://10.20.30.40:4318 \
  --allow-insecure
./scripts/install-linux-collector.sh --phase deploy
./scripts/install-linux-collector.sh --phase verify
```

O exemplo HTTP pressupõe rede privada e firewall allowlist. Em redes não
confiáveis, publique endpoints HTTPS e remova `--allow-insecure`.

Com cAdvisor e systemd:

```bash
./scripts/install-linux-collector.sh \
  --phase all \
  --host-name srv-docker-01 \
  --environment PRD \
  --team plataforma \
  --location dc-sp-01 \
  --metrics-endpoint https://ingest.example.net/prometheus/api/v1/write \
  --logs-endpoint https://ingest.example.net/loki/loki/api/v1/push \
  --otlp-endpoint https://ingest.example.net/otlp \
  --with-containers \
  --enable-service
```

## Validação central

No Prometheus/Grafana:

```promql
up{job="linux-node",instance="srv-app-01"}
node_uname_info{instance="srv-app-01"}
100 * (1 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])))
100 * (1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)
```

No Loki:

```logql
{job="linux-system",host_name="srv-app-01"}
```

Aceite somente quando existirem amostras novas, timestamps atuais, labels de
ambiente/time/localidade corretas e ausência de informação sensível.

## Operação e rollback

```bash
docker compose --env-file deploy/agents/linux/.env \
  -f deploy/agents/linux/docker-compose.yml logs --tail=200 alloy

docker compose --env-file deploy/agents/linux/.env \
  -f deploy/agents/linux/docker-compose.yml down
```

`down` preserva o WAL/positions do Alloy. Não remova o volume `alloy-data` até
confirmar que a fila foi drenada ou que a perda foi aceita. Para rollback,
restaure o diretório versionado anterior e execute novamente `deploy` e
`verify`.
