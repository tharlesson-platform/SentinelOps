# Rollout de coletores Linux nas VMs Azure TQI

Este runbook instala coleta de host com saída exclusiva para o gateway mTLS do
SentinelOps. Ele não abre Prometheus, Loki, Tempo, OTLP direto, Docker ou a
interface do SentinelOps para a Internet.

## Topologia aprovada

| Ativo | Perfil | Onda |
|---|---|---:|
| `tqi-platform` | host Linux, métricas, logs do sistema e logs JSON Docker | 1 |
| `easy-vm` | host Linux, métricas, logs do sistema e logs JSON Docker | 1 |
| `gitlab-vm` | host Linux, métricas, logs JSON Docker, cAdvisor e Beyla | 2 |
| `tqi-platform-edge` | host Linux e logs do sistema, sem Docker | 3 |

O gateway privado é `10.10.1.20`, publicado para os coletores como
`sentinelops` por DNS privado (ou, durante a transição, uma entrada idêntica
em `/etc/hosts`). A URL do gateway e `--tls-server-name` devem usar o mesmo
nome DNS: acessar o IP com SNI `sentinelops` provoca rejeição HTTP 421 pelo
proxy. Os coletores usam somente `8443/TCP` (OTLP HTTP, remote write e Loki)
e `44317/TCP` (OTLP gRPC), ambos com TLS 1.2+ e
certificado cliente individual. A allowlist do gateway deve conter somente os
IPs privados efetivamente aprovados de cada VM; uma regra final de descarte
para essas duas portas é obrigatória.

## Pré-requisitos e guardrails

1. Confirme rota privada e as duas portas a partir de cada VM.
2. Não execute durante um Run Command, atualização ou deploy Azure já ativo no
   mesmo host. Espere o término e registre o identificador da mudança alheia.
3. Confira que `12345`, `4317` e `4318` não estão ocupadas localmente. O
   compose do collector as publica apenas em `127.0.0.1`.
4. Se a imagem `sentinelops-alloy:1.18.1-patched.2` ainda não estiver no host,
   disponibilize pelo menos 25 GiB livres no filesystem usado pelo Docker ou
   carregue uma imagem pré-construída e verificada. A compilação no host cria
   cache transitório significativo; nunca a inicie se o Docker já reportar
   falta de espaço.
5. Use `--with-containers` apenas em hosts Docker. Esse perfil monta somente
   `DockerRootDir/containers` como leitura; não monta `docker.sock`.
   O instalador também mapeia o GID local do grupo `adm` ao Alloy para leitura
   dos logs de sistema com permissões `0640`; nenhum privilégio de escrita,
   socket Docker ou capability adicional é concedido.
6. Não habilite `--with-cadvisor` sem exceção formal por host: o perfil é
   privilegiado e não faz parte do baseline.
7. Antes do deploy, valide crescimento dos logs JSON. Um filesystem cheio
   invalida o rollout mesmo quando os containers de negócio ainda aparecem
   `running`; corrija a retenção sem reiniciar o daemon e preserve evidência
   suficiente para investigação.

## Instalação e aceite por onda

1. Emita um bundle por host com organização UUID real, nome de collector
   exclusivo, SAN do gateway e validade curta. A chave da CA nunca sai do
   gateway.
2. Transfira o bundle por canal autenticado, valide o SHA-256 no destino e
   execute `sudo ./install.sh` a partir do diretório extraído.
3. Antes da próxima VM, confirme `systemctl status sentinelops-collector`,
   `curl http://127.0.0.1:12345/-/ready` e os
   componentes Alloy saudáveis.
4. No SentinelOps, valide `up{job="linux-node",instance="<host>"}` e a
   chegada de logs sem linhas descartadas por credenciais. Só então avance a
   onda seguinte. O edge não participa do gate Docker e exige conectividade
   própria antes do baseline Linux.

## Rollback seguro

Pare e desabilite somente `sentinelops-collector`, preserve o bundle e
as evidências necessárias, e revogue o certificado do collector no gateway.
Não use `docker system prune`, reinicialização de Docker ou reboot como
rollback de rotina em hosts que executam workloads de aplicação.
