# Runbooks operacionais

Para toda ocorrência: declare ambiente/tenant, preserve evidência e horários em
UTC+BRT, identifique última release e mudança, não exponha secrets, e só feche
após validação funcional. Links abaixo usam Prometheus/Loki/Tempo/Pyroscope e
logs JSON do Control Plane.

## Agente offline

Confirme `lastHeartbeat`, relógio/DNS/rota/TLS do agente e revogação do token.
Compare outras localidades. Renove bootstrap apenas após revogar a identidade
antiga. Sucesso: três heartbeats consecutivos e job sintético real.

## Falha de ingestão de métricas

Verifique `/metrics`, target Prometheus, Alloy component health, remote-write e
rejeições por label/cardinalidade. Não aumente limites antes de identificar a
série causadora. Sucesso: amostra nova, query e alerta avaliando.

## Falha de ingestão de logs

Inspecione exportador OTLP, batch/redaction no Alloy, `/otlp/v1/logs` do Loki,
rate limit e schema. Envie log canário sem dado sensível e encontre-o por
`service_name` e `trace_id`.

## Falha de ingestão de traces

Confirme propagação W3C, OTLP 4317/4318, tail sampling, Tempo `/ready` e WAL.
Gere request com erro para preservar 100%; abra o trace no Grafana.

## Object storage indisponível

Pause jobs novos que produzam artefatos, preserve metadados, teste DNS/TLS/IAM e
capacidade, compare MinIO/S3 health. Não apague objetos. Retome com upload canário
e checksum; processe backlog com rate limit.

## PostgreSQL indisponível

API deve ficar not-ready. Confirme endpoint/TLS/pool/locks/replicação e espaço.
Evite restart em massa. Faça failover conforme provedor; valide migrations,
auditoria e uma transação por tenant antes de reabrir tráfego.

## Temporal indisponível

Gates novos retornam indisponível e pipelines não devem interpretar como PASS.
Verifique frontend, persistence, namespace, workers e task queues. Recupere e
confirme replay/continuação de um workflow existente.

## Alta cardinalidade

Use top series/labels, identifique owner e release, aplique drop/normalização
versionada no Alloy. Não elimine labels de correlação essenciais sem ADR. Valide
queda de séries e ausência de gaps críticos.

## Alto volume de logs

Identifique serviço, nível, mensagem e release. Aplique sampling/rate limit no
produtor/collector, preserve erros e auditoria, ajuste retention/lifecycle.

## Synthetic tests falhando

Separe alvo indisponível, problema de agente, credencial, seletor flaky e DNS/TLS.
Abra screenshot/trace/HAR sanitizados, execute uma vez manualmente e compare
localidades. Não marque flaky para esconder regressão.

## Quality gate inconclusivo

Confirme mínimo de amostras, backend indisponível e janela. Execute synthetic
controlado. Respeite a política `inconclusiveBehavior`; override requer dono,
justificativa, expiração e auditoria.

## Grafana indisponível

O gate pode continuar se queries diretas estiverem saudáveis, mas links ficam
degradados. Verifique DB/plugins/provisioning/datasources e recursos. Recupere e
abra dashboard, trace, log e profile correlacionados.

## Certificado próximo do vencimento

Confirme certificado servido externamente, cadeia/SAN/issuer e data em BRT/UTC.
Valide automação ACME/DNS/egress, renove sem remover outros domínios e teste com
cliente externo. Preserve rollback do certificado anterior.

## Disco próximo do esgotamento

Determine filesystem e taxa, retenção/WAL/compaction e arquivos órfãos. Não
remova WAL/chunks manualmente. Reduza ingestão, expanda volume com plano e
valide compaction, queries e headroom.

## Fila de jobs crescendo

Compare schedule-to-start, workers, concurrency, CPU/memória e dependências.
Pause fontes não críticas, escale dentro de limites e evite retry storm. Sucesso:
lag decrescente, jobs recentes e falhas explicadas.

## Falha no upload de artefatos

Valide tamanho, checksum, credencial, KMS, bucket policy e timeout. Artefatos
sensíveis devem continuar redigidos. Reenvie idempotentemente e vincule o novo
checksum à execução.

## Control Plane indisponível

Cheque `/healthz` e `/readyz`, PostgreSQL, Temporal, recursos, crash/OOM e última
release. Faça rollback manual da aplicação apenas com migration compatível.
Sucesso: login, catálogo, agent heartbeat e gate saudável reais.

