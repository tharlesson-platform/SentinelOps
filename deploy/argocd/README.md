# Bootstrap Argo CD

`application.yaml.example` é apenas um contrato e não deve ser aplicado. O
manifesto real é gerado somente depois que repositório, commit e values
produtivos passaram por validação local:

```bash
./scripts/render-argocd-application.sh \
  --repo-url https://github.com/acme/sentinelops.git \
  --revision COMMIT_SHA_DE_40_CARACTERES \
  --values-file values-acme-production.yaml \
  --output rendered/application.yaml
```

O arquivo de values precisa estar em `deploy/helm/sentinelops`, conter digests
publicados, URLs reais, allowlists e referências de Secrets. O renderer executa
`helm lint/template` e recusa branch móvel, arquivo ausente ou placeholders.
Antes do primeiro sync, instale External Secrets/ClusterSecretStore e confirme
que `sentinelops-secrets`, `sentinelops-upstream-ca`, TLS do servidor e CA dos
clientes existem. O hook PreSync cria ServiceAccount/NetworkPolicy dedicados e
executa a migração; ele falha fechado se a URL owner ou trust bundle faltar.
Após gerar, execute `argocd app diff --local` e anexe o diff ao change antes do
sync aprovado.
