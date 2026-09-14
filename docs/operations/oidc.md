# OIDC: contrato de produção

O SentinelOps usa OIDC para autenticação e os bindings persistidos no banco
para autorização. O modo local não é aceito em produção.

## Configuração

Configure no API e worker:

| Variável | Finalidade |
|---|---|
| `OIDC_ISSUER_URL` | issuer HTTPS que publica discovery e JWKS |
| `OIDC_CLIENT_ID` | identificação da aplicação web, mantida para consistência de configuração |
| `OIDC_API_AUDIENCE` | resource/audience exato aceito pela API |
| `OIDC_REQUIRED_SCOPE` | escopo exato exigido no access token, normalmente `sentinelops.api` |

No chart Helm, os correspondentes são `global.oidcIssuerURL`,
`global.oidcClientID`, `global.oidcAPIAudience` e
`global.oidcRequiredScope`. O perfil de produção falha no render se
audience ou escopo estiverem ausentes.

O navegador recebe apenas issuer, client ID e scopes solicitados. A API não
aceita ID token como substituto de access token: o token precisa satisfazer
issuer, assinatura, expiração, audience e scope.

## Provisionamento

1. Crie uma aplicação pública SPA com PKCE e callbacks HTTPS exatos.
2. Registre a API como resource/audience e emita `sentinelops.api` no
   access token.
3. Mapeie o claim `organization` para uma organização já provisionada.
4. Crie `users` e `role_bindings` com o `sub` imutável do provedor.
5. Habilite MFA, expiração curta e logout no IdP.

Não use e-mail como chave de binding. Não registre tokens, códigos de callback,
segredos de cliente ou payloads de identidade em tickets e evidências.

## Aceite de homologação

Valide com identidades reais, mas sem expor credenciais:

- token de outro issuer, audience ou sem o escopo recebe 401;
- organization não provisionada recebe 403;
- identidade sem binding recebe `role_binding_required`;
- binding existente continua válido após mudança de e-mail;
- logout, expiração e renovação de sessão retornam a SPA ao login;
- os fluxos de usuário e máquina não compartilham client secreto na SPA.
