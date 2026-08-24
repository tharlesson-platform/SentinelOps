# Instalação faseada em servidor Linux

Para o fluxo único do zero ao ambiente funcional, incluindo Docker, PKI,
migrações, mocks e provas, consulte
[Bootstrap Linux do zero](bootstrap-zero-to-running.md) e execute
sudo ./bootstrap-linux.sh.

Este guia entrega um perfil **single-node** reproduzível para laboratório,
homologação ou piloto em rede privada. Ele executa o Control Plane e todos os
backends locais em Docker Compose. Não substitui o perfil Kubernetes HA e não
remove os bloqueadores produtivos registrados em
[Produção](../operations/production.md).

## Compatibilidade

O instalador é POSIX shell e detecta o runtime do host em vez de depender do
nome da distribuição. O modo `--install-runtime` possui adaptadores para:

| Família | Gerenciador detectado | Init coberto |
|---|---|---|
| Debian/Ubuntu | `apt-get` | systemd |
| RHEL/Fedora/Amazon Linux | `dnf` ou `yum` | systemd |
| SUSE | `zypper` | systemd |
| Alpine | `apk` | OpenRC |
| Arch | `pacman` | systemd |

Outras distribuições funcionam se Docker Engine, Compose v2, `make`, `curl`,
`jq`, `openssl` e `htpasswd` já estiverem instalados. O bootstrap valida
`amd64`/`arm64`, 8 GiB de RAM e 40 GiB livres antes de subir o stack.

## Modelo de rede

Por padrão, Web e o gateway mTLS ficam em `127.0.0.1`. Os receivers diretos de
Alloy, Prometheus e Loki permanecem em loopback mesmo quando o gateway é
publicado; PostgreSQL e Temporal não recebem bind. Acesse a interface por
túnel SSH sem abrir portas:

```bash
ssh -L 3000:127.0.0.1:3000 usuario@servidor
```

Para coletores em outros hosts, use IP privado dedicado e firewall allowlist.
O bootstrap recusa `0.0.0.0` sem confirmação explícita e gera uma CA local
criptografada, certificado de servidor e gateway Caddy com TLS 1.2/1.3. Toda
ingestão remota exige certificado cliente com SAN SPIFFE organizacional; HTTP
remoto e `insecure_skip_verify` não são opções do instalador.

## Entrega do artefato

Entregue um tag/commit imutável e registre o checksum do pacote:

```bash
git archive --format=tar.gz --prefix=sentinelops/ -o sentinelops.tar.gz COMMIT_OU_TAG
sha256sum sentinelops.tar.gz > sentinelops.tar.gz.sha256
```

No servidor, valide o checksum e extraia em filesystem persistente. Não use
`/tmp` e não copie `.env` de outro ambiente:

```bash
sha256sum -c sentinelops.tar.gz.sha256
tar -xzf sentinelops.tar.gz
cd sentinelops
```

## Instalação faseada

Cada fase é idempotente e pode ser repetida após correção do problema.

```bash
./scripts/install-linux-server.sh --phase preflight
./scripts/install-linux-server.sh --phase runtime
./scripts/install-linux-server.sh --phase configure
./scripts/install-linux-server.sh --phase deploy
./scripts/install-linux-server.sh --phase seed
./scripts/install-linux-server.sh --phase verify
```

Se o host ainda não possuir Docker e utilitários:

```bash
./scripts/install-linux-server.sh --phase runtime --install-runtime
```

Instalação completa usando somente loopback:

```bash
./scripts/install-linux-server.sh --phase all --enable-service
```

Instalação em rede privada, expondo Web e somente o gateway mTLS no IP
`10.20.30.40`:

```bash
./scripts/install-linux-server.sh \
  --phase all \
  --web-bind 10.20.30.40 \
  --ingest-bind 10.20.30.40 \
  --ingest-server-name ingest.infra.example.net \
  --enable-service
```

O script não altera firewall, DNS ou balanceador. A PKI local serve para
piloto; em produção substitua-a por PKI corporativa. Autorize TCP 8443/44317
somente dos coletores e publique a Web por um reverse proxy TLS corporativo.

## O que cada fase faz

| Fase | Mutação | Critério de saída |
|---|---|---|
| `preflight` | nenhuma | arquitetura, RAM e disco aprovados |
| `runtime` | opcionalmente instala pacotes | Docker e Compose respondem |
| `configure` | gera `.env`, CA criptografada, certificado e dashboards | secrets 0600 e PKI validada |
| `deploy` | build, lock SHA256 e `compose --profile secure-ingest` com API/worker duplicados | configuração Compose válida e imagens próprias imutáveis |
| `seed` | catálogo e cenário demo idempotentes | dados iniciais registrados |
| `verify` | consultas HTTP/readiness | API, Web e backends ready |
| `service` | unit systemd, quando disponível | serviço habilitado no boot |

Em OpenRC ou outro init, as restart policies do Compose continuam ativas; a
unit automática é criada somente em systemd.

## Configuração e credenciais

O arquivo `.env` é exclusivo daquele servidor, tem modo `0600` e não deve ser
enviado para Git, pipeline, chat ou backup sem criptografia. Consulte as
credenciais conscientemente no próprio host:

```bash
make credentials
```

Antes de uso compartilhado, troque autenticação local por OIDC validado, ajuste
`ALLOWED_ORIGIN`, use TLS e remova Keycloak/Mailpit/demo do perfil publicado.

## Prova funcional pós-deploy

```bash
make doctor
make seed
make prove-local
make prove-ha
make prove-resilience
./scripts/prove-gates.sh
./scripts/prove-observational-gates.sh
./scripts/prove-secure-agent.sh # após emitir um certificado de prova
docker compose --env-file .env -f deploy/compose/docker-compose.yml --profile secure-ingest ps
```

Critérios mínimos de aceite do single-node:

- todos os serviços persistentes `running` e health checks verdes;
- API `/readyz`, Web `/healthz` e backends respondendo;
- seed repetido sem duplicação;
- release saudável `PASS`, política ausente `INCONCLUSIVE` e limite excedido `FAIL`;
- cliente sem certificado recusado, token de agente não reutilizável e fingerprint/tenant vinculados;
- os três mocks localizados e correlacionados em Prometheus, Loki, Tempo e
  Pyroscope, com evidência JSON emitida por `make prove-local`;
- uma réplica de API pode ser interrompida e restaurada por `make prove-ha`,
  sem confundir essa tolerância de processo com HA contra perda do host;
- cold start do Pyroscope respeita o orçamento de 240 segundos e o guard de
  readiness detecta indisponibilidade;
- nenhum secret impresso ou arquivo `.env` com permissão mais ampla que 0600.

## Dados persistentes

O stack utiliza volumes Docker para PostgreSQL, MinIO, Grafana, Prometheus,
Loki, Tempo, Pyroscope e Alloy. `make down` preserva dados. `make reset` remove
os volumes e é destrutivo; não use em ambiente com dados que precisam ser
retidos.

## Upgrade e rollback

1. Gere uma passphrase de backup exclusiva em secret manager/arquivo 0600.
2. Execute `./scripts/upgrade-local.sh upgrade --passphrase-file ARQUIVO --confirm UPGRADE`.
3. O script retém as imagens correntes, cria backup age autenticado, promove o
   rebuild, roda `doctor` e faz rollback automático em falha.
4. Exercite o caminho antes da janela com `--simulate-failure`.
5. Para rollback manual use o manifest preservado em `artifacts/upgrades/` e
   `upgrade-local.sh rollback --manifest ARQUIVO --confirm ROLLBACK`.
6. Migration incompatível exige roll-forward ou restore; imagem anterior não
   desfaz schema.

O runbook completo de entrega está em
[delivery-runbook](../operations/delivery-runbook.md).
