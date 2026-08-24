# Onboarding de servidores Linux

## Caminho recomendado: bundle autocontido

No servidor central, emita um pacote com identidade mTLS exclusiva:

    ./scripts/create-linux-collector-bundle.sh \
      --organization acme-prod \
      --collector-name srv-app-01 \
      --gateway-url https://ingest.internal.example:8443 \
      --tls-server-name ingest.internal.example \
      --environment production \
      --team payments \
      --location dc1

Transfira o arquivo tar.gz e seu SHA-256 por canal autenticado. No host alvo,
confira o hash, extraia e execute sudo ./install.sh. O pacote instala Docker e
Compose, constrói o Alloy corrigido, coleta métricas do host e logs do sistema,
abre OTLP somente em loopback para as aplicações e ativa Systemd ou OpenRC.

O bundle contém somente CA pública, certificado e chave do collector. Ele não
contém chave da CA, passphrase ou credenciais administrativas. O certificado
vale 30 dias; gere e distribua novo bundle antes da expiração.

Para containers, acrescente --with-containers ao gerar o bundle. Esse perfil é
privilegiado e deve ser aprovado por host; ele não é ativado por padrão.

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
locais para evitar conflito com o Alloy central e use o gateway mTLS:

```bash
./scripts/install-linux-collector.sh --phase all \
  --metrics-endpoint https://ingest.local:8443/api/v1/write \
  --logs-endpoint https://ingest.local:8443/loki/api/v1/push \
  --otlp-endpoint https://ingest.local:8443 \
  --tls-ca-file /etc/sentinelops/ca.crt \
  --tls-cert-file /etc/sentinelops/client.crt \
  --tls-key-file /etc/sentinelops/client.key \
  --tls-server-name ingest.local \
  --admin-port 12346 --otlp-grpc-port 14317 --otlp-http-port 14318 \
  --enable-service
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
  --ingest-bind 10.20.30.40 \
  --ingest-server-name ingest.infra.example.net
./scripts/install-linux-server.sh --phase deploy
./scripts/install-linux-server.sh --phase verify
```

No firewall, permita somente coletores autorizados para TCP `8443` (OTLP HTTP,
remote write e Loki) e `44317` (OTLP gRPC). As portas diretas 4317/4318/9090/
3100 continuam em loopback e não devem ser publicadas.

## Emitir identidade de collector

Crie primeiro um bootstrap token autenticado. A resposta contém o
`organizationId` e exibe o token uma única vez. Em seguida, no servidor da CA,
emita um certificado exclusivo para a mesma organização e nome:

```bash
./scripts/bootstrap-pki.sh issue-collector \
  --ca-dir deploy/gateway/pki \
  --passphrase-file .sentinelops/secrets/pki-ca-passphrase \
  --organization ORGANIZATION_UUID \
  --name srv-app-01 \
  --output-dir artifacts/collector-srv-app-01
```

Transfira `ca.crt`, `client.crt` e `client.key` por canal seguro. A chave deve
permanecer 0600. Certificado de outra organização, sem SAN SPIFFE válido ou
fora da validade não recebe rota no gateway. Rotacione emitindo novo
certificado e novo token one-time, registre novamente e só então revogue o
anterior.

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
  --metrics-endpoint https://ingest.infra.example.net:8443/api/v1/write \
  --logs-endpoint https://ingest.infra.example.net:8443/loki/api/v1/push \
  --otlp-endpoint https://ingest.infra.example.net:8443 \
  --tls-ca-file /etc/sentinelops/ca.crt \
  --tls-cert-file /etc/sentinelops/client.crt \
  --tls-key-file /etc/sentinelops/client.key \
  --tls-server-name ingest.infra.example.net
./scripts/install-linux-collector.sh --phase deploy
./scripts/install-linux-collector.sh --phase verify
```

Com cAdvisor e systemd:

```bash
./scripts/install-linux-collector.sh \
  --phase all \
  --host-name srv-docker-01 \
  --environment PRD \
  --team plataforma \
  --location dc-sp-01 \
  --metrics-endpoint https://ingest.example.net:8443/api/v1/write \
  --logs-endpoint https://ingest.example.net:8443/loki/api/v1/push \
  --otlp-endpoint https://ingest.example.net:8443 \
  --tls-ca-file /etc/sentinelops/ca.crt \
  --tls-cert-file /etc/sentinelops/client.crt \
  --tls-key-file /etc/sentinelops/client.key \
  --tls-server-name ingest.example.net \
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
