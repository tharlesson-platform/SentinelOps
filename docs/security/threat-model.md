# Threat model STRIDE

## Ativos e fronteiras

Ativos: identidade, políticas, evidências, telemetria, artefatos, referências de
secrets e audit log. Fronteiras: navegador/API, CI/webhooks, API/agentes,
Control Plane/backends e agentes/alvos privados.

| Ameaça | Exemplo | Controle |
|---|---|---|
| Spoofing | agente ou webhook falso | bootstrap de uso único, mTLS/OIDC, HMAC, timestamp e nonce |
| Tampering | alterar gate/evidência | políticas versionadas, checksum de artefato e auditoria append-only |
| Repudiation | override sem autoria | subject, motivo, expiração, request ID e eventos imutáveis |
| Information disclosure | token em log/screenshot | redaction de headers/campos, secretRef, CSP e limites de artefato |
| Denial of service | query ampla/cardinalidade | rate limit, timeout, quotas, allowlist e limites por tenant |
| Elevation of privilege | acesso entre tenants | RBAC por escopo, `organization_id`, RLS em produção e testes de isolamento |

## Requisitos de produção

- TLS externo e mTLS para agentes; rotação e revogação documentadas.
- Containers não-root, filesystem read-only, seccomp, capabilities removidas,
  NetworkPolicy e egress explícito.
- OIDC com MFA delegado; tokens curtos, cookies `HttpOnly/Secure/SameSite` e
  CSRF em fluxos baseados em cookie.
- Object storage criptografado, versionado e com lifecycle; PostgreSQL com PITR.
- Imagens promovidas por digest, SBOM, scan e assinatura verificada na admissão.

## Riscos residuais locais

Compose é uma demonstração em uma única máquina, sem HA e com tráfego interno
em rede Docker. Não deve ser exposto à Internet nem promovido diretamente.

