## O que mudou

Descreva a mudança e o motivo.

## Risco e impacto

- Ambiente(s) afetado(s):
- Dados, tenancy, segurança ou disponibilidade:
- Compatibilidade e migração:

## Evidência

- [ ] `make check`
- [ ] Teste específico da mudança
- [ ] Manifests/IaC renderizados ou validados, quando aplicável
- [ ] Documentação atualizada
- [ ] Nenhum secret, chave privada, dump ou artefato foi incluído

Cole apenas resultados sanitizados ou links para checks do SHA atual.

## Rollback

Explique como reverter código, configuração e dados, e quais sinais confirmam a
recuperação.

## Checklist de produção

- [ ] Imagem por digest, SBOM, scan e assinatura
- [ ] IdP, secrets, políticas e backends validados no alvo
- [ ] Readiness, SLO, backup/restore e rollback comprovados
- [ ] Aprovação do CODEOWNER
