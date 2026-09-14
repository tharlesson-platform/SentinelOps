# SPEC-013 — APM e correlação dos quatro sinais

**Fase:** E2/E3 · **Estado:** IN_PROGRESS · **Responsável:** SRE/Aplicações

## Problema, escopo e contratos

Kits Go, Java/Spring, .NET, Node e Python precisam propagar W3C, emitir RED,
spans de erro e correlacionar métricas, logs, traces e profiles suportados.
Sampling e redação acontecem antes de exportar; payload e PII não viram
atributos/labels por padrão.

## Dados, UX, autorização e operação

Serviço, versão, ambiente, asset e timestamp UTC são contratos de telemetria.
Trace ID é correlacionável com logs, mas não concede acesso fora do tenant.
Mudança de SDK tem baseline de CPU/p95 e rollback por configuração.

## Critérios e evidência

- **AC-1301:** Dada transação conhecida entre três serviços e uma dependência,
  quando executa, então traces/logs/métricas mostram a mesma jornada.
- **AC-1302:** Dado erro ou latência num salto, quando investigado, então o
  serviço causador é localizável com janela e fonte.
- **AC-1303:** Dado payload com PII, quando instrumentado, então não é
  exportado por atributo/label padrão.
- **AC-1304:** Dado kit ativado, quando comparado ao baseline, então impacto
  p95 e CPU fica registrado antes de expandir.

Teste local usa aplicações demonstrativas; aceite E2 exige aplicação TQI
autorizada. Rollout por serviço e rollback desativa instrumentação sem remover
telemetria já retida.

## Corte implementado e lacunas

O bootstrap APM gera kits por runtime com atributos de recurso estáveis,
propagação W3C e um modo de prova OTLP para métricas, logs e traces. Ele aceita
HTTP somente em loopback, exige HTTPS sem credencial/query/fragmento para
destinos remotos e para Faro, e exige IP literal para resolução mTLS. O Alloy
local remove `authorization`, `cookie` e `user.email` antes da saída e aplica
tail sampling de erros, tráfego sintético e baseline.

O harness cobre geração segura e nega endpoints remotos inseguros. Ainda faltam
aplicação TQI autorizada, transação com três serviços reais, baseline de CPU/p95
e validação de PII por runtime para concluir os critérios em alvo.
