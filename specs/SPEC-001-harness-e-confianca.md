# SPEC-001 — Harness e confiança operacional

**Estado:** IN_PROGRESS · **Responsável:** Plataforma SentinelOps

## Problema do operador

O portal pode apresentar dados fixos ou checks sem prova. O operador precisa
saber o que foi medido, em qual janela, por qual fonte e qual verificação é
executável.

## Escopo e contratos

- O overview só mostra valores derivados da API; para telemetria/gates sem
  consulta, mostra `Sem dados` e a fonte/timestamp disponível.
- O harness registra checks, specs e evidências. Scripts não executam comandos
  fornecidos pelo usuário como shell arbitrário.
- Migrations usam ledger com checksum e lock transacional.

## Modelo, UX, autorização e operação

`schema_migrations` preserva versão, checksum e data. A UI não altera
autorização; as APIs continuam usando RBAC existente. Upgrade aborta se o
checksum histórico divergir. Rollback de schema é roll-forward compatível,
nunca apagamento automático de dados.

## Critérios

- **AC-001:** Dado que não há série consultada, quando o overview abre, então
  disponibilidade, degradações e score são `Sem dados`, sem números ilustrativos.
- **AC-002:** Dado que uma migration aplicada mudou de conteúdo, quando a
  migração roda, então falha antes de aplicar qualquer novo arquivo.
- **AC-003:** Dado que um check exige ambiente externo ausente, quando o
  comando roda, então retorna `BLOCKED` e código diferente de zero.

## Testes e evidências

`make harness-validate-specs`, `make harness-check`; integração de migration
usa banco isolado e é registrada como `BLOCKED` enquanto as URLs não existirem.
