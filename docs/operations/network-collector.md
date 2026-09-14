# Collector de rede e FortiGate

## Fronteira de segurança

O collector em `deploy/agents/network` usa `snmp_exporter` com SNMPv3 `authPriv` e Alloy. Ele não recebe alvo da UI, não aceita CIDR ou hostname no manifesto e não publica a porta do exporter. Cada execução nasce de uma allowlist versionada de IPs literais, com `asset_id`, site, ambiente, time e módulo explícitos. Isso reduz DNS rebinding, SSRF pelo endpoint `/snmp` e varredura acidental; não substitui ACL/VRF/firewall do site.

O perfil opcional `--with-syslog` recebe UDP e TCP na porta escolhida para um IP
literal não-loopback do collector. Ele fica desligado por padrão e exige ACL no
firewall do site apenas dos emissores aprovados; não há bind em `0.0.0.0` no
host. Eventos seguem por mTLS para Loki, levam labels estáveis de site/time e
descartam linhas com marcador aparente de credencial antes da saída.

O formato `auths` separado de `modules`, expansão de segredo por ambiente e o uso de SNMPv3 `authPriv` seguem a documentação oficial do [snmp_exporter](https://github.com/prometheus/snmp_exporter). O exporter deve ficar em um host com alcance direto à VLAN/VRF de gerenciamento — proxy HTTP corporativo não transporta SNMP.

## Preparação

1. Crie uma credencial SNMPv3 somente leitura para cada domínio de gerenciamento aprovado. Não use comunidade v2c nem credenciais de escrita.
2. Prepare um arquivo `.env` com modo `0600` a partir de `.env.example`; não o versione nem o passe por argumento de shell.
3. Prepare `targets.json`, contendo somente os IPs dos ativos autorizados, a partir de `targets.json.example`. O renderer recusa hostname, URL, CIDR, módulo fora da allowlist, IP e asset duplicados.
4. Emita certificado mTLS exclusivo para o collector e confira a janela de validade antes da mudança.

## Instalação

```sh
./scripts/install-network-collector.sh --phase preflight

./scripts/install-network-collector.sh --phase all \
  --targets /caminho/aprovado/targets.json \
  --env-file deploy/agents/network/.env \
  --tls-ca-file /caminho/ca.crt \
  --tls-cert-file /caminho/client.crt \
  --tls-key-file /caminho/client.key
```

Após aprovar IP do listener, porta e ACL origem→collector, inclua Syslog:

```sh
./scripts/install-network-collector.sh --phase all --with-syslog \
  --targets /caminho/aprovado/targets.json \
  --env-file deploy/agents/network/.env \
  --tls-ca-file /caminho/ca.crt \
  --tls-cert-file /caminho/client.crt \
  --tls-key-file /caminho/client.key
```

Nesse perfil, `.env` precisa conter `SENTINEL_LOGS_ENDPOINT` HTTPS sem query,
`SENTINEL_SYSLOG_BIND_ADDRESS` IP unicast do host e
`SENTINEL_SYSLOG_PORT`. A porta administrativa do listener continua em
loopback. O instalador não muda ACL, FortiGate ou configuração de emissor.

A fase `configure` apenas renderiza a allowlist e valida o Compose; não abre conexão SNMP. `deploy` inicia os containers. `verify` confirma readiness do Alloy em loopback e lista o estado dos containers. Ele não considera essa evidência como saúde dos ativos: correlacione no gateway cada `asset_id`, timestamp, contador de interface e erro SNMP antes de aceitar a onda.

## Rollout e rollback

Comece por um único FortiGate/roteador de homologação em VLAN de gerenciamento, com ACL UDP/161 estritamente origem→destino. Compare uptime, interfaces e erros/discards com a API/console do fornecedor para amostras selecionadas. Valide explicitamente timeout, credencial recusada e indisponibilidade; nenhum deles pode aparecer como healthy.

Para interromper a coleta sem tocar no equipamento, execute:

```sh
SENTINEL_NETWORK_ENV_FILE=.env docker compose \
  --env-file deploy/agents/network/.env \
  -f deploy/agents/network/docker-compose.yml down
```

O comando remove apenas os containers locais; não modifica configuração, ACL,
credencial ou firmware de nenhum equipamento. Syslog exige homologar emissor,
ACL, TLS e evento conhecido por site. Traps, LLDP e descoberta ampla continuam
pendentes e exigem desenho por site.
