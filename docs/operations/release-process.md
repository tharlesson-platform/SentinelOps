# Release, assinatura e promoção

O workflow `release-images` separa três gates:

1. `verify`: compila as nove imagens para amd64 e arm64 e bloqueia findings
   HIGH/CRITICAL antes de qualquer publicação;
2. `stage`: após aprovação do Environment GitHub `production`, publica somente
   tags `candidate-<commit>`, gera SBOM/provenance, assina e verifica o digest;
3. `promote`: só inicia quando todos os nove candidatos terminaram. Faz
   preflight de todos os digests/assinaturas, promove tags SemVer e SHA, gera
   `release-manifest.json`, assina o manifesto e cria a GitHub Release.

Configure o Environment `production` com required reviewers e impeça
self-review. Sem essa configuração, o campo `environment` não oferece gate
humano suficiente. Proteja também tags `v*` e permita execução somente a partir
de commits revisados em `main`.

A existência de uma tag de container não prova release completa. O marcador
atômico consumível por GitOps é a GitHub Release com
`release-manifest.json` + `release-manifest.bundle.json`. Se a promoção falhar
no meio, nenhuma release é criada; tags parciais não podem ser usadas no
values produtivo.

Antes de configurar um remoto, os controles equivalentes que não dependem do
GitHub são executáveis localmente:

```bash
make validate-release
```

Esse alvo valida os workflows com actionlint, renderiza o perfil distribuído,
exige digests, PDB/HPA/anti-affinity/NetworkPolicy e assina/verifica um
manifesto dos IDs SHA256 locais com chave efêmera. A chave privada é destruída;
somente manifesto, bundle e chave pública permanecem em `artifacts/release/`.
Isso não substitui a assinatura keyless do registry.

Antes do deploy:

```bash
cosign verify-blob \
  --bundle release-manifest.bundle.json \
  --certificate-identity-regexp '^https://github.com/ORG/REPO/.github/workflows/release.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  release-manifest.json
```

Copie os digests do manifesto para os values; admission policy deve recusar
imagem sem assinatura correspondente. Guarde URL da run, SHA, aprovador,
manifesto e resultado da verificação no change.

Após configurar `origin`, execute `./scripts/prove-remote-ci.sh`. O gate falha
se não encontrar CI terminal verde no HEAD ou se a branch padrão não exigir
reviews e status checks.
