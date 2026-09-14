# SPEC-002 — Identidade OIDC e RBAC autoritativo

**Fase:** E1 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma SentinelOps

## Problema do operador

Um token emitido para a SPA, sem audience de API, escopo ou vínculo RBAC
provisionado, não deve abrir acesso ao control plane. E-mail e nome exibido
também não são identificadores estáveis.

## Escopo e contratos

No perfil OIDC, a API verifica assinatura/JWKS, issuer, expiração e audience
via `OIDC_API_AUDIENCE`. Ela exige o escopo exato
`OIDC_REQUIRED_SCOPE`, preserva o `sub` como identidade e requer o claim
`organization` para localizar o tenant provisionado.

O papel efetivo não vem de groups ou role claim: a cada requisição protegida,
o servidor consulta `role_bindings` no tenant. A ausência do vínculo produz
`403 role_binding_required`. A SPA solicita `openid profile sentinelops.api`
por padrão, com PKCE, renovação de sessão e logout delegados ao IdP.

## Dados, autorização e UX

Os vínculos usam `organization_id + subject + role + scope`; o subject é o
`sub` imutável do provedor. O e-mail pode ser atributo de exibição, nunca
chave de autorização. A UI exibe login corporativo quando o arquivo público de
configuração informa `authMode=oidc`; no modo local, estritamente de
desenvolvimento, permanece o login bootstrap.

## Instalação, limites e dependências

O IdP precisa de uma aplicação pública SPA e de um resource/audience separado
para a API, ambos com callbacks HTTPS explícitos. Requer discovery/JWKS
acessível, MFA/política de sessão no IdP, organização provisionada e bindings
persistidos. O frontend não recebe client secret.

## Critérios

- **AC-0201:** Dado token válido para outro audience ou sem
  `sentinelops.api`, quando acessa rota protegida, então recebe 401.
- **AC-0202:** Dado token com `sub` e organization válidos, mas sem binding
  persistido, quando acessa rota protegida, então recebe
  `403 role_binding_required`.
- **AC-0203:** Dado e-mail alterado no IdP, quando o `sub` permanece igual,
  então o binding existente continua sendo usado.
- **AC-0204:** Dado logout, token expirado ou usuário removido do IdP, quando
  tenta renovar ou chamar a API, então a sessão não recupera privilégio.

## Testes, rollout, rollback e evidência

Os testes locais cobrem a configuração obrigatória de audience, matching de
scope e a resolução de binding por subject. Na homologação, execute casos
positivos e negativos de issuer, audience, escopo, tenant, MFA, expiração,
logout e remoção de acesso, armazenando apenas IDs de execução e resultados.

Para rollout, publique primeiro uma aplicação IdP de homologação e bindings
mínimos; mantenha `AUTH_MODE=local` apenas fora de produção. O rollback é
reverter o arquivo de valores e a aplicação IdP para a configuração anterior,
sem desabilitar validação de audience/scope para contornar falhas.
