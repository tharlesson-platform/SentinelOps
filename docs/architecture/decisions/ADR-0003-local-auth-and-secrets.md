# ADR-0003 — Autenticação local e secrets efêmeros

- Status: aceita
- Data: 2026-08-17

## Decisão

O perfil local permite login próprio com senha e chave JWT geradas por
`make bootstrap` em `.env`. Produção rejeita `AUTH_MODE=local` e exige OIDC.
YAML armazena somente `secretRef`.

## Consequências

O quickstart não contém credenciais estáticas. Perder `.env` invalida sessões e
exige regeneração; isto é desejável em desenvolvimento.

