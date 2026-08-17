# ADR-0002 — Quality gates determinísticos

- Status: aceita
- Data: 2026-08-17

## Decisão

Checks são funções versionadas sobre evidências delimitadas por tempo, amostra
e tenant. `FAIL` em check obrigatório reprova; ausência de amostra suficiente
gera `INCONCLUSIVE`; IA não altera o resultado.

## Consequências

Cada decisão é reproduzível e auditável. Overrides exigem identidade,
justificativa e expiração. Rollback automático permanece desabilitado.

