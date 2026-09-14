# Evidência de rollout dos hosts Docker Azure — 2026-09-14

## Decisão

Status da frota Docker Azure: **PARTIAL / REJECTED para encerramento**.

Dois dos três hosts Docker estão coletando host, logs Docker, métricas de
containers e APM eBPF. O terceiro host, `gitlab-vm`, foi recuperado de uma
saturação de disco e integralmente preparado, mas não foi ativado porque ainda
não possui um certificado mTLS exclusivo assinado pela CA do gateway. Reutilizar
o certificado de outro host foi deliberadamente rejeitado.

## Inventário revalidado

| VM | IP privado | Docker | Collector | Evidência atual |
|---|---|---|---|---|
| `tqi-platform` | `172.48.10.26` | 29.1.3, 24/26 containers running | ACTIVE, ENABLED | Alloy ready, 0 componentes unhealthy, quatro containers do collector running, zero restarts |
| `easy-vm` | `172.48.10.24` | 28.1.1, 23/25 containers running | ACTIVE, ENABLED | Alloy ready, 0 componentes unhealthy, quatro containers do collector running, zero restarts |
| `gitlab-vm` | `172.48.10.25` | 20.10.17, 2/2 containers running | NOT ACTIVATED | preflight/runtime PASS, imagens carregadas, arquivos verificados, portas locais livres, gateway alcançável |
| `tqi-platform-edge` | `172.48.30.4` | ausente | INACTIVE | não é host Docker; `8443/44317` inacessíveis a partir da VM |

O IP público `40.70.25.71:22` termina em `tqi-platform`, não no edge. Ele não
foi usado como evidência de acesso ou cobertura do `tqi-platform-edge`.

## Mudanças executadas

### `tqi-platform`

- Change IDs `20260914T140229Z` e `20260914T140325Z`.
- A configuração persistente passou a declarar `containers`, `cadvisor` e
  `beyla`; a unit foi regenerada sem `--remove-orphans`.
- O instalador e a biblioteca comum foram atualizados para os hashes
  `62ca6875...b055` e `b3d7e2a6...8129`.
- Os quatro IDs dos containers do collector, seus `StartedAt` e os 20
  containers de negócio permaneceram inalterados.
- Certificado individual válido até 2026-10-10 e gateway
  `sentinelops:8443/44317` alcançável.
- Risco residual: filesystem raiz em 81%, com 5,8 GiB livres. Não executar build
  local de imagem nesse host. O timer de rotação Docker ainda está inativo e os
  três maiores logs JSON medem aproximadamente 1,84 GiB, 650 MiB e 531 MiB. A
  primeira rotação deve ser precedida de arquivamento controlado para `/mnt`
  ou expansão do disco, evitando esgotar o filesystem durante `copytruncate`.

### `easy-vm`

- Change ID `20260914T140516Z`.
- Corrigida a divergência entre unit e `.env`: os três perfis agora estão
  persistidos como `true` e a unit contém os mesmos três perfis.
- Alloy ready, zero componentes unhealthy, zero restarts nos quatro containers
  do collector e 19 containers de negócio preservados.
- Certificado individual válido até 2026-10-10 e ambas as portas do gateway
  alcançáveis.
- Change ID `20260914T143946Z`: instalada a política de rotação dos logs JSON
  Docker a cada 15 minutos, com limite de 100 MiB, três rotações,
  `copytruncate`, compressão e prioridades reduzidas de CPU/I/O. O timer ficou
  `enabled/active`, a execução inicial terminou com `Result=success` e os 23
  containers mantiveram os mesmos IDs e estados. Os logs rotacionados foram
  preservados em `.1`; o filesystem permaneceu em 72%, com 36 GiB livres.
- Hashes da política, service e timer: `73049f83...00a2`,
  `9e4d4a5a...5942` e `d67962fe...ff4a7`.

### `gitlab-vm`

- O filesystem raiz estava em 100% por um único log JSON Docker de
  57.388.449.792 bytes. Os dados persistentes em `/app/gitlab/data` não foram
  alterados.
- Change ID `20260914T141353Z`: os 100 MiB finais foram preservados em
  `/mnt/sentinelops-evidence/gitlab-json-tail-100MiB-20260914T141353Z.log`, modo
  0600, SHA-256
  `b7ba3ef06be4a9cd17411eda16afb1cec97dc02b9f016805e24620ca5c3db235`.
  O arquivo JSON ativo foi truncado e o uso caiu para 57%, liberando 54 GiB.
  O histórico anterior a esses 100 MiB não é recuperável por esta mudança.
- Change ID `20260914T141436Z`: instalado timer de rotação a cada 15 minutos,
  limite de 100 MiB e três rotações. Unit/timer passaram `systemd-analyze
  verify`; execução manual terminou com `Result=success`.
- Adicionado `10.10.1.20 sentinelops` ao `/etc/hosts`, com backup em
  `/mnt/sentinelops-evidence`; `8443` e `44317` passaram a responder.
- Instalados somente `make`, `jq` e `apache2-utils`; Docker Engine, Compose e
  Buildx existentes foram preservados.
- GitLab recuperou `healthy`, endpoint interno retornou HTTP 200, os 15 serviços
  Omnibus ficaram `run`, GitLab e Runner mantiveram ID/`StartedAt` e zero
  restarts.
- Alloy corrigido, Beyla e Alpine foram carregados. Para Alloy, a lista de
  layers e a superfície de configuração produziram os mesmos hashes na origem
  e no destino. Beyla e Alpine foram conferidos pelos digests declarados no
  Compose.
- Arquivos foram preparados em
  `/opt/sentinelops-collectors/sentinelops-collector-gitlab-vm`, `root:root`,
  com hashes idênticos ao workspace. A unit do collector não foi criada.

## Gate restante do GitLab

É necessário acesso administrativo atual ao host `sentinelops` (`10.10.1.20`)
para emitir `cert-source/ca.crt`, `client.crt` e `client.key` com:

```text
spiffe://sentinelops/organizations/aa3df87f-dbca-4784-af73-a0b1ab77b5bd/collectors/gitlab-vm
```

A CA do checkout local não corresponde à CA do gateway de produção. Depois da
emissão, o fluxo autorizado é:

1. transferir somente os três arquivos TLS por SSH autenticado;
2. executar `configure` com host `gitlab-vm`, ambiente `production`, localização
   `azure-tqi-gitlab`, endpoints `https://sentinelops:8443` e os perfis
   `containers`, `cadvisor` e `beyla`;
3. executar `deploy`, `verify` e `service` nessa ordem;
4. confirmar Alloy ready, todos os componentes saudáveis e nenhum restart dos
   containers `gitlab`/`gitlab-runner`;
5. provar freshness em Prometheus, Loki e APM para `host_name=gitlab-vm`.

## Freshness ponta a ponta atual

Consultas read-only executadas em 2026-09-14 contra o Data Plane retornaram:

| Sinal | `tqi-platform` | `easy-vm` |
|---|---:|---:|
| `up{job="linux-node"}` | 1 | 1 |
| idade da amostra | 9,5 s | 12,1 s |
| `container_last_seen` | 24 | 23 |
| séries `target_info` Beyla | 65 | 28 |
| eventos Docker nos últimos 5 min | 2.119 | 22.120 |

O contador agregado de falhas de remote write retornou zero. `gitlab-vm` não
apareceu nessas consultas, como esperado para um collector ainda não ativado.
Essa ausência é `BLOCKED`, não `PASS`.

As mesmas provas revelaram que `10.10.1.20:9090`, `:3100` e `:3000` estão
alcançáveis diretamente pela rede privada. Prometheus reporta
`web.listen-address=0.0.0.0:9090`; Loki respondeu consultas e recuperou
`/ready=200`. Isso contradiz o isolamento descrito no runbook e é bloqueador de
produção, mesmo dentro da VPN, pois permite contornar o gateway tenant-aware.

## Rollback

- `tqi-platform`: restaurar a unit e `.env` com sufixo do change ID e executar
  `systemctl daemon-reload`; não reiniciar Docker.
- `easy-vm`: restaurar o `.env.before-20260914T140516Z` se a declaração de
  perfil precisar ser revertida. Para a rotação, desabilitar o timer e restaurar
  os arquivos guardados em `/var/backups/sentinelops/20260914T143946Z`; os
  containers não foram recriados.
- `gitlab-vm`: como o collector não foi ativado, não há serviço para remover.
  Para reverter a rotação, desabilitar o timer e restaurar arquivos datados de
  `/mnt/sentinelops-evidence`. O recorte de 100 MiB preservado deve ser retido
  até o fechamento do incidente de disco.
- Em qualquer host, o rollback do collector deve parar somente
  `sentinelops-collector` e revogar seu certificado. É proibido usar
  `docker system prune`, reiniciar o daemon ou reiniciar a VM como atalho.

## Bloqueios fora do escopo Docker

- `tqi-platform-edge`: Docker ausente e gateway inacessível. Seu rollout Linux
  exige correção de rota/firewall e certificado próprio.
- `tqi-platform`: o collector está funcional, mas a raiz em 81% e a ausência de
  retenção para logs Docker mantêm risco operacional alto. Arquivar/rotacionar
  de forma controlada ou ampliar o disco antes de crescimento adicional.
- A plataforma SentinelOps completa continua sem aprovação de produção pelos
  demais gates do plano: IdP, HA distribuída, promoção assinada, DR e integrações
  externas ainda não são fechados por este rollout.
