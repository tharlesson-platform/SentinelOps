# Política de segurança

## Reportar uma vulnerabilidade

Não abra uma issue pública com detalhes exploráveis, credenciais ou dados de
terceiros. Use o recurso privado **Security > Advisories > Report a
vulnerability** deste repositório. Inclua versão/SHA, impacto, reprodução
mínima e mitigação sugerida.

Não existe SLA público de correção enquanto o projeto estiver em fase inicial.
O mantenedor fará triagem, confirmará o recebimento e coordenará divulgação e
correção de acordo com severidade e exposição.

## Versões suportadas

Somente a branch `main` e a release estável mais recente recebem correções. O
perfil Compose é laboratório/piloto single-node e não deve ser exposto como
produção.

## Resposta a secrets expostos

1. Revogue e rotacione o secret na origem.
2. Identifique acessos e o período de exposição.
3. Remova o material do histórico com coordenação dos mantenedores.
4. Invalide clones, caches e artefatos afetados quando aplicável.
5. Documente causa, impacto e prevenção sem reproduzir o valor sensível.

## Princípios do projeto

- TLS/mTLS na ingestão remota e identidade tenant-bound.
- Credenciais por referência, nunca embutidas em cenários ou repositório.
- Runtime PostgreSQL não-superusuário com FORCE RLS.
- Imagens fixadas por digest, SBOM, scan e assinatura antes de produção.
- OIDC, MFA, políticas de admission, restore e DR comprovados no alvo real.
- Logs, traces, payloads e respostas externas são dados não confiáveis.

Consulte [threat model](docs/security/threat-model.md) e os
[runbooks](docs/runbooks/operational-response.md) antes de expor endpoints.
