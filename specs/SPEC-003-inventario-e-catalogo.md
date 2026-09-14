# SPEC-003 — Inventário e catálogo unificado

**Fase:** E1/E2 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma SentinelOps

## Problema do operador

Hostname, IP e nome exibido mudam. Sem um ID estável, importações repetidas
duplicam ativos e a investigação perde owner, site, origem e histórico.

## Escopo e contratos

O recurso `Asset` é tenant-scoped e contém `assetId` estável, nome,
tipo, site, owner, ambiente, lifecycle, origem, `lastSeen` e labels. O
upsert é idempotente por `organization_id + asset_id`; renomear atualiza o
mesmo registro. Os estados permitidos são `active`, `stale` e `retired`,
que não equivalem a saudável.
Conectores usam `POST /api/v1/assets/reconcile` com snapshot completo,
`source` DNS-safe e no máximo mil itens; somente após persistir o snapshot
os ativos antes ativos e ausentes daquela mesma fonte tornam-se `stale`.
Collectors usam `POST /api/v1/agents/{id}/inventory-reconcile` somente com
token de agente, mTLS em produção e capability `inventory:write`; tenant e
source são derivados no servidor.

## Dados, autorização e UX

A migration cria `assets` com RLS forçado, índice único e validação de
formato. A API exige `asset:read` ou `asset:write` e usa o tenant da
identidade, nunca organização enviada no corpo. O CLI expõe list, get e apply;
a tela de catálogo deve consumir a mesma API, sem inventar estados.

## Limites e dependências

A entrada não aceita IP como identidade obrigatória. Conectores VMware, Azure,
AWS, SNMP e collectors poderão usar UUID, serial ou resource ID normalizado
sem barras como `assetId`;
cada integração precisa manter credenciais read-only, paginação, checkpoint e
fonte explícita.

## Critérios

- **AC-0301:** Dadas duas aplicações do mesmo `assetId`, quando o nome muda,
  então há um único ativo e o ID interno é preservado.
- **AC-0302:** Dado tenant A, quando consulta ou tenta gravar ativo do tenant
  B, então RLS não devolve nem aceita o registro.
- **AC-0303:** Dado ativo sem heartbeat após a política futura de freshness,
  quando é marcado stale, então não aparece como saudável.
- **AC-0304:** Dado importador sem fonte ou ID estável, quando chama a API,
  então recebe erro de validação.

## Testes, rollout, rollback e evidência

Teste unitário cobre validação e integração PostgreSQL cobre RLS e upsert.
Homologue uma fonte autorizada em modo read-only antes de importar produção.
O rollback é parar o importador e reverter o consumidor; não remova histórico
nem reutilize `assetId` para outro recurso.
