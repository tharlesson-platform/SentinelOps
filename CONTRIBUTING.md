# Como contribuir

Obrigado por contribuir com o SentinelOps. Mudanças pequenas, reproduzíveis e
com evidência operacional são mais fáceis de revisar e reverter.

## Fluxo de contribuição

1. Crie uma branch a partir de `main`: `feature/<tema>`, `fix/<tema>`,
   `docs/<tema>` ou `chore/<tema>`.
2. Faça uma alteração focada e atualize contratos, exemplos e documentação
   afetados.
3. Execute `make check`. Para infraestrutura ou release, execute também
   `make validate-release`.
4. Não inclua `.env`, tokens, certificados privados, dumps, artefatos ou dados
   pessoais. Use somente arquivos `.example` com valores fictícios.
5. Abra um Pull Request com risco, evidência, rollback e impacto preenchidos.
6. Aguarde todos os checks no SHA atual e a revisão do CODEOWNER.

## Commits

Use Conventional Commits: `feat:`, `fix:`, `docs:`, `test:` ou `chore:`.

## Regras técnicas

- Go novo ou alterado deve passar por `gofmt`, testes com race detector e vet.
- Frontend deve passar por testes, lint, build e audit configurados no CI.
- Manifests devem renderizar e validar; Terraform deve passar por fmt,
  init sem backend e validate.
- Dependências e imagens devem permanecer fixadas; releases usam digest, SBOM,
  scan e assinatura.
- Decisões arquiteturais duradouras exigem ADR em
  `docs/architecture/decisions/`.
- O CI valida e publica; produção é reconciliada pelo fluxo GitOps aprovado.

## Segurança

Não abra uma issue pública contendo uma vulnerabilidade explorável. Siga
[SECURITY.md](SECURITY.md). Um secret exposto deve ser revogado antes de limpar
o histórico; apenas removê-lo do commit não o torna seguro novamente.
