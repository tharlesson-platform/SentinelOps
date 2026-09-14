# SPEC-005 — Frota de agentes e gateway de inventário

**Fase:** E1/E2 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma SentinelOps

## Problema do operador

Agentes precisam coletar inventário sem receber privilégios administrativos da
plataforma. Uma credencial vazada ou um agente desativado não pode continuar
publicando heartbeat, telemetria ou ativos.

## Escopo e contratos

O bootstrap token é de uso único, possui expiração e pode ser vinculado a um
nome de agente. O cadastro gera token de agente armazenado somente como hash.
O agente usa esse token apenas nas rotas próprias de heartbeat e reconciliação
de inventário. Em produção, a reconciliação também exige prova de mTLS
validada por um proxy confiável, capability `inventory:write`, snapshot
completo e no máximo 1.000 ativos.

Um administrador com `agent:write` pode executar
`POST /api/v1/agents/{id}/revoke`. A ação é idempotente, gera auditoria e
marca `revoked_at`; agentes revogados deixam de aparecer na lista ativa e
não podem enviar heartbeat ou inventário. Um novo bootstrap explícito é a
única forma de reativar uma identidade.

## Segurança, rollout e limites

O tenant do inventário é obtido do agente autenticado, jamais de campo do
corpo. O proxy de mTLS deve remover o cabeçalho de autorização vindo da
internet e adicioná-lo apenas depois de validar certificado de cliente. Tokens
e certificados não são registrados em logs, evidências ou documentação.

O rollout começa com um agente não crítico, credenciais read-only e um
snapshot sanitizado. O rollback é revogar o agente, parar o processo e
preservar inventário/auditoria para investigação. Isto não instala o agente
em hosts de produção nem substitui a homologação de gateway, CA e rotação.

Para o perfil Docker, logs JSON usam somente o mount read-only do caminho
derivado de `DockerRootDir/containers`; um diretório ausente encerra a
configuração em vez de iniciar uma coleta vazia. Métricas cAdvisor continuam
opt-in, em perfil separado e privilegiado, exigindo exceção explícita por host.

## Critérios

- **AC-0501:** Bootstrap reutilizado, expirado ou revogado é recusado.
- **AC-0502:** Após revogação, heartbeat e inventário do agente retornam
  não autorizado e a consulta administrativa não o lista como ativo.
- **AC-0503:** Reconciliação parcial, sem capability ou sem mTLS em produção
  é recusada sem marcar ativos existentes como stale.
- **AC-0504:** Uma organização não consegue revogar ou observar agente de
  outra organização.

## Evidência local

O teste de integração PostgreSQL valida timestamp de revogação e a tentativa
cross-tenant como não encontrada sob RLS. A validação em alvo requer gateway
mTLS, IdP e um agente descartável autorizados.
