# Instalação faseada em servidor Linux

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

Por padrão, Web, OTLP, Prometheus e Loki ficam em `127.0.0.1`. PostgreSQL,
Temporal e MinIO não recebem bind remoto adicional. Acesse a interface por
túnel SSH sem abrir portas:

```bash
ssh -L 3000:127.0.0.1:3000 usuario@servidor
```

Para coletores em outros hosts, use IP privado dedicado e firewall allowlist.
O bootstrap recusa `0.0.0.0` para ingestão sem confirmação explícita. HTTP
remoto é aceitável somente dentro de rede privada controlada; PRD exige gateway
TLS/mTLS ou autenticação forte antes dos receivers.

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

Instalação em rede privada, expondo a Web e receivers no IP `10.20.30.40`:

```bash
./scripts/install-linux-server.sh \
  --phase all \
  --web-bind 10.20.30.40 \
  --ingest-bind 10.20.30.40 \
  --enable-service
```

O script não altera firewall, DNS, certificado ou balanceador. Autorize no
firewall somente os IPs dos coletores e publique a Web por um reverse proxy
TLS corporativo.

## O que cada fase faz

| Fase | Mutação | Critério de saída |
|---|---|---|
| `preflight` | nenhuma | arquitetura, RAM e disco aprovados |
| `runtime` | opcionalmente instala pacotes | Docker e Compose respondem |
| `configure` | gera `.env` 0600 e dashboards | secrets locais presentes sem exposição |
| `deploy` | build e `compose up -d` | configuração Compose válida |
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
./scripts/prove-gates.sh
docker compose --env-file .env -f deploy/compose/docker-compose.yml ps
```

Critérios mínimos de aceite do single-node:

- todos os serviços persistentes `running` e health checks verdes;
- API `/readyz`, Web `/healthz` e backends respondendo;
- seed repetido sem duplicação;
- release saudável `PASS` e falha controlada `FAIL`;
- telemetria localizada em Prometheus, Loki, Tempo e Pyroscope;
- nenhum secret impresso ou arquivo `.env` com permissão mais ampla que 0600.

## Dados persistentes

O stack utiliza volumes Docker para PostgreSQL, MinIO, Grafana, Prometheus,
Loki, Tempo, Pyroscope e Alloy. `make down` preserva dados. `make reset` remove
os volumes e é destrutivo; não use em ambiente com dados que precisam ser
retidos.

## Upgrade e rollback

1. Registre SHA, imagens/digests e estado atual com `docker compose ps`.
2. Faça backup e execute restore test conforme
   [lifecycle](../operations/lifecycle.md).
3. Extraia a nova versão em outro diretório e reutilize secrets somente por
   mecanismo seguro, após comparar novas variáveis.
4. Execute `configure`, `deploy` e `verify`.
5. Em falha sem migration incompatível, volte ao diretório/commit anterior e
   execute `deploy` e `verify`.
6. Em migration incompatível, interrompa e use o plano de roll-forward ou
   restore aprovado; não suponha que reverter a imagem reverte o banco.

O runbook completo de entrega está em
[delivery-runbook](../operations/delivery-runbook.md).
