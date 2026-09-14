# SPEC-017 — MCP do SentinelOps

**Fase:** E2/E3 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma/Segurança

## Problema, escopo e contratos

MCP oferece ferramentas estreitas e read-only sobre serviços autorizados, não
shell genérico, SQL livre ou transporte de telemetria. O servidor negocia
protocolo/capabilities e autentica no próprio servidor. Primeiras tools:
assets, métricas, logs, traces, SLOs, incidentes, mudanças, runbooks e plano
diagnóstico, com schemas fechados, paginação e limites.

## Estado implementado e próximos cortes

O corte inicial entrega `sentinelops-mcp` por stdio usando o SDK MCP oficial.
As tools `assets_search` e `asset_get` são read-only, possuem validação local
de parâmetros, paginação limitada e delegam à API, sem conexão direta ao banco.
O token da API continua sendo a fonte de tenant e RBAC. O transporte exige TLS
fora de loopback e suporta CA/certificado cliente para gateway mTLS sem opção
de ignorar a validação do certificado.

Ainda não estão implementadas neste corte as tools de métricas, logs, traces,
SLOs, incidentes, mudanças, runbooks e diagnóstico. Elas só podem ser expostas
depois de cada API subjacente ter autorização, limite, contrato de freshness e
teste de isolamento próprios.

## Dados, UX, autorização e operação

Tenant, identidade e RBAC vêm do contexto autenticado, nunca do argumento do
modelo. Respostas carregam request ID, fonte, janela, freshness, truncation,
partial e warnings. Origin, audience, SSRF, confused deputy, timeout e quota
são controlados no transporte remoto.

## Critérios e evidência

- **AC-1701:** Dado cliente de homologação, quando inicializa, então lista e
  chama uma tool read-only real com schema válido.
- **AC-1702:** Dado timeout, cursor ou erro parcial, quando responde, então
  envelope preserva limite, warning e request ID.
- **AC-1703:** Dado escopo insuficiente, quando chama tool, então servidor nega
  mesmo que o cliente a exiba.
- **AC-1704:** Dado texto malicioso em log, quando retornado, então não altera
  autorização nem aciona outra tool.

Teste de isolamento cobre sintaxe de query e recurso indireto. Rollout exige
allowlist de tools no cliente e no servidor; rollback desabilita capability.
