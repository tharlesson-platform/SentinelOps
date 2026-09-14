# Inventário cloud read-only

`apps/cloudinventory` produz um snapshot compatível com o agente SentinelOps a
partir da CLI oficial já autenticada. Não recebe segredo na linha de comando,
não executa shell por entrada e só substitui o arquivo de saída depois de todas
as páginas terminarem com sucesso. Cada arquivo de escopo corresponde a uma
única identidade de agente e a uma única subscription Azure ou account/região
AWS: isso impede que uma falha parcial marque ativos de outro escopo como
`stale`.

## Preparação e execução

1. Copie o exemplo para um arquivo local protegido (`chmod 600`), preencha
   somente o escopo já aprovado e mantenha-o fora do Git. Use
   `deploy/agents/cloud/azure.scope.example.json` ou
   `deploy/agents/cloud/aws.scope.example.json`.
2. Autentique a CLI por identidade federada ou managed identity. Não coloque
   client secret, access key ou token no arquivo de escopo.
3. Gere o snapshot e apenas depois deixe o agente já registrado publicá-lo:

```sh
./scripts/discover-cloud-inventory.sh \
  --config /etc/sentinelops/azure.scope.json \
  --output /var/lib/sentinelops-agent/inventory.json
```

4. No agente de inventário, configure `SENTINEL_INVENTORY_FILE` para esse
   caminho, inclua `inventory:write` em `AGENT_CAPABILITIES` e defina
   `SENTINEL_INVENTORY_MAX_AGE` menor que duas execuções programadas, por
   exemplo `20m` para um job a cada dez minutos. Arquivo vencido não é enviado.
   O agente preserva tenant e fonte a partir da sua própria credencial; esses
   valores não vêm do snapshot cloud.

O comando usa `az account show` antes do Azure Resource Graph e exige que a
subscription retornada seja exatamente a aprovada. Consulta somente
`Resources`, com paginação por `skipToken`; IDs internos de ativos são hashes
estáveis do Resource ID, enquanto subscription e resource group ficam como
metadados de origem. Tags e propriedades arbitrárias não são importadas para
evitar levar possíveis dados pessoais ou segredos ao catálogo.

No AWS o comando compara `sts get-caller-identity` com a conta declarada e usa
somente `configservice:DescribeConfigurationRecorders`,
`configservice:DescribeConfigurationRecorderStatus` e
`configservice:SelectResourceConfig`. Ele recusa publicar se o AWS Config não
estiver gravando todos os tipos suportados naquela região, se houver paginação
incompleta, recurso fora de account/região ou mais de mil ativos. Não há
`AssumeRole`, `Put*`, `Create*`, `Update*` ou `Delete*` neste collector; a
renovação de credencial curta permanece responsabilidade do perfil federado da
AWS CLI.

## Permissões mínimas e limites

Azure requer uma identidade já limitada à subscription autorizada, com Reader e
permissão de consulta Resource Graph. AWS requer somente as quatro ações de
leitura acima no profile indicado. Valide o papel efetivo com a equipe cloud
antes do primeiro agendamento; este repositório não cria role assignment,
trust policy, Diagnostic Settings ou nenhum recurso cloud.

O snapshot é um inventário de recursos. Métricas e logs de Azure Monitor,
CloudWatch, CloudTrail, EventBridge e AWS Health ainda não fazem parte deste
collector. Erro de acesso, token expirado, throttling ou AWS Config incompleto
encerra a execução sem trocar o snapshot anterior; o limite de idade no agente
impede que esse arquivo anterior seja reapresentado como descoberta recente.
Throttling e falhas transitórias recebem até três retries, com backoff
exponencial e jitter entre 125 ms e 2 s; `AccessDenied` e credencial expirada
não recebem retry cego. O log registra somente a classificação sanitizada e o
atraso, nunca a resposta da CLI.

## Rollout, rollback e homologação

Faça uma onda por scope: subscription/account-região de homologação, confira
asset ID, nome, tipo, owner, ambiente e `observedAt` contra a API nativa, depois
libere o timer. O rollback é desabilitar o timer e revogar/remover a
credencial/profile de descoberta; não exclua recursos cloud nem histórico do
catálogo.

O teste local cobre paginação Azure, identificação estável, validação de conta
AWS, recusa de recurso fora do scope e recusa de configuração desconhecida. A
homologação ainda precisa de uma subscription e conta sandbox autorizadas,
incluindo 429, expiração federada e revisão independente das permissões.
