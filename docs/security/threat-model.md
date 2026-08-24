# Threat model STRIDE

## Ativos e fronteiras

Ativos: identidade, políticas, evidências, telemetria, artefatos, referências de
secrets e audit log. Fronteiras: navegador/API, CI/webhooks, API/agentes,
Control Plane/backends e agentes/alvos privados.

| Ameaça | Exemplo | Controle |
|---|---|---|
| Spoofing | agente, proxy ou webhook falso | bootstrap one-time, mTLS tenant/name-bound, segredo API-gateway, OIDC, HMAC, timestamp e nonce |
| Tampering | backup/gate/evidência | age autenticado, checksums, políticas versionadas e auditoria |
| Repudiation | override sem autoria | subject, motivo, expiração, request ID e eventos imutáveis |
| Information disclosure | token em log/screenshot | redaction de headers/campos, secretRef, CSP e limites de artefato |
| Denial of service | query ampla/cardinalidade | rate limit, timeout, quotas, allowlist e limites por tenant |
| Elevation of privilege | acesso entre tenants | role binding persistido, FORCE RLS em 41 tabelas, role runtime não-superusuária, SAN SPIFFE, tenant/name sobrescritos pelo gateway e testes negativos |
| SSRF | probe alcança metadata/admin | http(s) absoluto, allowlist exata/wildcard e redirect revalidado |

## Requisitos de produção

- TLS externo e mTLS para agentes; segredo compartilhado API-gateway com no
  mínimo 32 bytes, nome/tenant do SAN vinculados, rotação e revogação documentadas.
- Containers não-root, filesystem read-only, seccomp, capabilities removidas,
  NetworkPolicy e egress explícito.
- OIDC com MFA delegado; tokens curtos, cookies `HttpOnly/Secure/SameSite` e
  CSRF em fluxos baseados em cookie.
- Object storage criptografado, versionado e com lifecycle; PostgreSQL com PITR.
- Imagens promovidas por digest, SBOM, scan e assinatura verificada na admissão.

## Riscos residuais locais

Compose é single-node, sem HA, e os backends locais operam em modo single-
tenant; o isolamento de transporte é provado, mas a separação física/lógica do
storage deve ser habilitada em Mimir/Loki/Tempo produtivos. O gateway mTLS pode
ser publicado em rede controlada; UI/backends diretos não devem ir à Internet.
O PostgreSQL aplica FORCE RLS às tabelas diretas e filhas, além dos filtros
explícitos no store/API. Migração e runtime usam credenciais distintas; uma URL
runtime com superusuário ou `BYPASSRLS` reprova o gate.
