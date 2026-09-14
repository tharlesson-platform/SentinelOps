# Quota inicial por organização

Owner: Plataforma/SRE. A API aplica `TENANT_REQUESTS_PER_MINUTE` (padrão 600)
depois de autenticar a organização; heartbeats e reconciliação de inventário de
agentes autenticados usam a mesma quota. Ao exceder, devolve HTTP 429 com
`tenant_quota_exceeded` e `Retry-After: 60`. A métrica
`sentinel_tenant_quota_rejections_total{scope="api|agent"}` não inclui o ID do
tenant para não criar cardinalidade aberta; o alerta
`TenantRequestQuotaExceeded` é entregue ao time Plataforma.

O contador usa uma linha PostgreSQL por organização/minuto, com `UPSERT`
atômico e RLS, portanto é compartilhado entre réplicas da API. Ele não cobre
ainda ingestão de sinais, gateway, storage, cache, IA ou MCP. Antes de
produção, defina quotas por origem/sinal/query/modelo e valide isolamento sob
carga nos componentes restantes.

## Consulta de catálogo

Além do teto geral, `CATALOG_QUERIES_PER_MINUTE` (padrão 120) aplica uma janela
PostgreSQL própria a `GET /api/v1/assets` e `GET /api/v1/assets/{assetId}` por
organização. A classificação vem da rota autenticada no servidor, não de um
header controlado pelo cliente; por isso as consultas MCP delegadas à API
recebem o mesmo limite. O excesso retorna `429 tenant_quota_exceeded`,
`Retry-After: 60` e incrementa
`sentinel_tenant_quota_rejections_total{scope="catalog-query"}` sem expor o
tenant como label.

Esta é somente a primeira quota por consulta: não comprova limite de séries,
bytes, backends de logs/traces, ferramentas IA, storage ou ingestão.

## Resposta operacional

1. Identifique organização e rota pelo request ID nos logs protegidos; não
   adicione o tenant como label de métrica.
2. Verifique release, loop de agente e retry storm antes de elevar o teto.
3. Registre owner, motivo, duração e impacto. Ajuste
   `TENANT_REQUESTS_PER_MINUTE` somente em mudança aprovada e faça rollback
   para o valor anterior se o consumo prejudicar os demais tenants.
4. Para um cluster com mais de uma API, ensaie concorrência contra o PostgreSQL
   e monitore o impacto do contador antes de promover a configuração.

## Evidência de integração

Execute `make harness-integration` com URLs distintas para migration e runtime.
Quando o banco isolado estiver apenas em uma rede Compose, defina também
`SENTINELOPS_TEST_DOCKER_NETWORK` com o nome validado da rede; o harness não
exige publicar PostgreSQL no host. O teste prova que duas admissões do tenant A
esgotam seu teto, uma terceira é recusada e o tenant B permanece elegível, tanto
para a quota geral como para `catalog-query`.
