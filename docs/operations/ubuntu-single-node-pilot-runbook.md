# Runbook — SentinelOps em Ubuntu single-node

Este runbook registra a instalação reproduzível do perfil **piloto privado**
em um único host Ubuntu. Ele não é uma aprovação para produção: o perfil
Docker Compose é deliberadamente local/single-node e o gate de produção está
em [Produção](production.md).

## Escopo e acesso

- Host de piloto: `10.10.1.20` (nome: `sentinelops`), Ubuntu 26.04.1 LTS;
- instalação: `/opt/sentinelops`, de propriedade `root:root` e modo `0750`;
- control plane: somente `127.0.0.1`; gateway mTLS: `10.10.1.20`;
- acesso da interface: túnel SSH, sem abrir portas de aplicação:

```bash
ssh -L 3000:127.0.0.1:3000 usertqi@10.10.1.20
```

Não registre senha, conteúdo de `.env`, chaves privadas ou passphrases neste
documento, em tickets, chat ou repositório. O arquivo `.env` e a PKI gerada no
host devem ficar em modo `0600` e não são portáveis entre ambientes.

## Gateway para coletores externos

O IP do host é persistido por Netplan como `10.10.1.20/24`, com gateway
`10.10.1.1` e DNS `10.10.1.2`/`10.10.1.3`. O certificado mTLS do gateway
possui SAN DNS `sentinelops` e SAN IP `10.10.1.20`.

Somente os endpoints abaixo são publicados no IP privado do host:

| Porta TCP | Uso |
|---:|---|
| 8443 | OTLP HTTP, Prometheus remote write e Loki push com mTLS |
| 44317 | OTLP gRPC com mTLS |

O web control plane, Prometheus, Loki, Tempo, Grafana, banco e interfaces de
admin continuam em loopback. Antes de distribuir bundles, o firewall entre os
coletores Azure e este gateway deve aceitar apenas os IPs/CIDRs confirmados dos
hosts autorizados. Não publique 4317, 4318, 9090, 3100 ou 3000 externamente.

## Proveniência da entrega inicial

| Item | Valor |
|---|---|
| Base Git inspecionada | `f9675f82f763a91b17d98a08ed22b2603dac9beb` |
| SHA-256 do pacote transferido | `a3003cddd5be29a22d72dde212f98a6df8064814f03c9a98e566df8072090410` |
| Registro no host | `/opt/sentinelops/RELEASE-SOURCE.sha256` |

O pacote foi criado da árvore de trabalho entregue para esta instalação. Como
a árvore local possuía mudanças ainda não promovidas, o checksum do pacote é a
proveniência operacional deste piloto; uma promoção posterior deve partir de
commit/tag limpo, imagem por digest e assinatura verificável.

## Bootstrap usado

No diretório `/opt/sentinelops`, execute o fluxo idempotente abaixo. O modo
`--without-seed` evita dados de demonstração antes do onboarding de ativos
reais.

```bash
sudo ./bootstrap-linux.sh \
  --web-bind 127.0.0.1 \
  --ingest-bind 10.10.1.20 \
  --ingest-server-name sentinelops \
  --ingest-server-ip 10.10.1.20 \
  --without-seed
```

O bootstrap valida recursos, configura segredos exclusivos do host, gera PKI
local de piloto, prepara/fecha as imagens locais, sobe o perfil
`secure-ingest`, executa provas funcionais e instala `sentinelops.service`.
Antes de usar coletores remotos, substitua a PKI de piloto por PKI corporativa,
publique somente por rede privada allowlisted e emita certificados individuais
com SAN SPIFFE organizacional.

## Operação diária

```bash
sudo systemctl status sentinelops --no-pager
sudo journalctl -u sentinelops -n 200 --no-pager
cd /opt/sentinelops
sudo docker compose --env-file .env \
  -f deploy/compose/docker-compose.yml \
  --profile secure-ingest ps
sudo make doctor
```

Para reiniciar após uma intervenção controlada:

```bash
sudo systemctl restart sentinelops
sudo systemctl is-active sentinelops
```

Não use `make reset`: ele remove volumes e é destrutivo. Não exponha uma porta
Docker apenas criando regra de firewall; o daemon deve permanecer com logs
rotacionados e o gateway deve ter allowlist explícita.

## Backup, atualização e rollback

Antes de alterar release, crie uma passphrase exclusiva em secret manager ou
arquivo local `0600`, fora do pacote de backup. Siga o runbook de
[lifecycle](lifecycle.md) para backup/restore e o de
[entrega](delivery-runbook.md) para upgrade. O rollback de imagens somente é
seguro quando as migrations são compatíveis; caso contrário aplique o plano
aprovado de roll-forward ou restore.

```bash
cd /opt/sentinelops
sudo ./scripts/upgrade-local.sh rollback \
  --manifest artifacts/upgrades/rollback-AAAAMMDDTHHMMSSZ.manifest \
  --confirm ROLLBACK
```

Um backup só conta como DR depois de um restore em projeto/host isolado e com
RTO/RPO observados.

## Evidências a registrar por alteração

- versão de Docker, Compose e sistema operacional;
- checksum do pacote, commit/tag e digests das imagens;
- `systemctl`, `docker compose ps`, `make doctor` e provas funcionais;
- permissões de `.env` e PKI, sem valores secretos;
- portas em escuta, regras de firewall e caminho de acesso aprovado;
- backup/restore, rollback e owner da janela de mudança.

## Bloqueadores para produção

Não classifique este host como produção enquanto faltarem: cluster Kubernetes
HA, imagens publicadas/assinadas por digest, GitOps com aprovação, OIDC/MFA e
RBAC por tenant, DNS/ingress HTTPS, PKI corporativa/mTLS de agentes, CIDRs de
ingresso/egresso, banco com PITR/restore comprovado, object storage resiliente,
observabilidade/SLOs e DR ensaiado. A lista completa e os comandos de render
estão em [Produção](production.md).

## Onboarding inicial de ativos

Após o aceite deste piloto, cadastre cada ativo com owner, tenant, criticidade,
ambiente, IP/FQDN, tipo de coletor, método de autenticação de menor privilégio,
CIDR permitido e plano de rollback. Emita um token one-time e certificado mTLS
por coletor; nunca reutilize credenciais administrativas nem centralize agente
em um host sem identidade do alvo.
