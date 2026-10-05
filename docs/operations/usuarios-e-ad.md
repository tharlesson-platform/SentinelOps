# Usuários, AD e acesso ao SentinelOps

Este runbook descreve o caminho recomendado para homologação e produção.
Em produção, o SentinelOps deve usar OIDC; o login local é somente para
desenvolvimento e testes.

## Resultado esperado

- O usuário é criado e desabilitado/habilitado no AD, que continua sendo a
  autoridade da conta.
- O grupo de segurança do AD chamado `sentinelops` controla o acesso à
  aplicação.
- Um IdP OIDC (Keycloak, Microsoft Entra ID ou outro provedor corporativo)
  federa o AD e emite o token para o SentinelOps.
- As demais ferramentas do conjunto confiam no mesmo IdP. Isso fornece SSO;
  cada ferramenta valida seu próprio token/audience e não deve reutilizar o
  access token de outra aplicação.

## Criar ou liberar um usuário

1. No AD, crie o usuário seguindo a política corporativa de identidade e
   exija troca de senha no primeiro acesso, quando aplicável.
2. Adicione o usuário ao grupo de segurança `sentinelops`. Prefira o grupo
   por SID/objectGUID no IdP; o nome exibido pode mudar.
3. Aguarde ou execute a sincronização AD→IdP conforme a janela corporativa.
4. Confirme, sem copiar credenciais, que o usuário aparece como membro do
   grupo no IdP e que o claim `groups` contém exatamente `sentinelops`.
5. Faça login pela URL do SentinelOps usando o botão **Entrar com conta
   corporativa**.
6. Valide uma leitura autorizada e confirme que um usuário fora do grupo
   recebe `403`/acesso negado.
7. Para remover o acesso, remova o usuário do grupo no AD, aguarde a
   propagação e invalide as sessões no IdP conforme a política corporativa.

Não crie usuários diretamente no banco para acesso corporativo. O vínculo de
autorização deve usar o `sub` imutável emitido pelo IdP, nunca e-mail como
chave.

## Configurar o SentinelOps

No API e no worker, configure pelo gerenciador de segredos:

```text
AUTH_MODE=oidc
OIDC_ISSUER_URL=https://login.microsoftonline.com/<tenant-id>/v2.0
OIDC_CLIENT_ID=<application-client-id>
OIDC_API_AUDIENCE=api://<application-client-id>
OIDC_REQUIRED_SCOPE=sentinelops.api
OIDC_REQUIRED_GROUP=<sentinelops-group-object-id>
```

No frontend, publique em `apps/web/public/config.js` somente os dados públicos
do cliente SPA:

```js
window.__SENTINEL_CONFIG__ = {
  authMode: "oidc",
  oidcAuthority: "https://login.microsoftonline.com/<tenant-id>/v2.0",
  oidcClientId: "<application-client-id>",
  oidcScope: "openid profile groups api://<application-client-id>/sentinelops.api",
};
```

Registre no IdP os callbacks HTTPS exatos da instalação, habilite Authorization
Code + PKCE e configure o mapper para emitir `groups`. Não coloque client
secret na SPA.

## Integração AD com Keycloak

1. Em **User Federation**, adicione o LDAP/Active Directory por LDAPS;
   valide CA, hostname, bind de leitura e base DN com a equipe de identidade.
2. Configure o mapeamento de grupos AD para o grupo do realm `sentinelops` e
   publique esse grupo no claim `groups` do client `sentinelops-web`.
3. Crie o client público `sentinelops-web` com PKCE e o client/resource da API
   com audience `sentinelops-api` e scope `sentinelops.api`.
4. Configure o SentinelOps com as variáveis acima e valide discovery, JWKS,
   issuer, audience, scope, grupo e organização.
5. Cadastre cada uma das demais ferramentas como client/resource no mesmo
   realm/tenant. Todas devem redirecionar para o mesmo IdP, mas manter
   audiences, scopes e permissões próprios.

Microsoft Entra ID pode substituir o Keycloak: nesse caso, use um Enterprise
Application/ grupos atribuídos, emita o grupo `sentinelops` no token e use o
issuer, audience e scope publicados pelo tenant. A escolha entre Keycloak e
Entra deve ser feita pela equipe responsável pelo diretório; o SentinelOps não
faz bind LDAP diretamente pelo navegador.

## Sessão

O login local de desenvolvimento não expira mais por timeout do SentinelOps.
No modo OIDC, o SentinelOps aceita somente tokens válidos do IdP; expiração,
revogação, logout e renovação silenciosa continuam sob controle do AD/IdP.
Isso evita manter uma sessão corporativa indefinida quando a conta for
desabilitada ou removida do grupo.

## Checklist de aceite

- [ ] Usuário membro de `sentinelops` entra com SSO.
- [ ] Usuário fora de `sentinelops` é rejeitado.
- [ ] Mudança de e-mail não quebra o vínculo pelo `sub`.
- [ ] Remoção no AD deixa de permitir novos tokens e a sessão é revogada
      conforme a política do IdP.
- [ ] As outras ferramentas usam o mesmo IdP, com clients/audiences próprios.
- [ ] Nenhum segredo, token, senha ou payload de identidade foi colocado em
      Git, ticket ou log.
