# Versões dos componentes

Verificadas inicialmente em 2026-08-20 e revalidadas nos registries em
2026-09-03. Todas
as imagens do Compose usam tags exatas; para produção, o pipeline deve promover
por digest após scan e assinatura.

| Componente | Versão | Motivo / fonte oficial |
|---|---:|---|
| Go | 1.26.6 | toolchain estável, multiarch; <https://go.dev/dl/> |
| Node.js | 26.7.0 | build do frontend; <https://nodejs.org/en/download> |
| Alpine runtime | 3.23.5 | base fixada e atualizada após CVEs corrigíveis; <https://hub.docker.com/_/alpine> |
| NGINX | 1.29.6 | runtime do frontend com digest multiarch; <https://hub.docker.com/_/nginx> |
| gRPC-Go | 1.83.1 | corrige CVE-2026-84304 no control plane e no Alloy recompilado; <https://github.com/grpc/grpc-go/releases/tag/v1.83.1> |
| Terraform | 1.15.9 | IaC DEV/STG/PRD; <https://github.com/hashicorp/terraform/releases/tag/v1.15.9> |
| kubeconform | 0.8.0 | validação OpenAPI dos manifests renderizados; <https://github.com/yannh/kubeconform/releases> |
| React | 19.2.8 | UI; <https://www.npmjs.com/package/react> |
| TypeScript | 7.0.2 | tipagem; <https://www.npmjs.com/package/typescript> |
| Vite | 8.2.1 | build; <https://www.npmjs.com/package/vite> |
| PostgreSQL | 18.3 | banco relacional; <https://www.postgresql.org/docs/release/> |
| Temporal | 1.29.7 | versão mais recente publicada na imagem oficial `auto-setup`; o SDK 1.47.0 mantém compatibilidade; <https://hub.docker.com/r/temporalio/auto-setup/tags> |
| Temporal UI | 2.53.3 | inspeção local; <https://github.com/temporalio/ui/releases> |
| MinIO | RELEASE.2025-09-07T16-13-09Z | versão mais recente publicada na imagem oficial; <https://hub.docker.com/r/minio/minio/tags> |
| Grafana | 13.1.3 | visualização e alertas; <https://github.com/grafana/grafana/releases> |
| Grafana Alloy | 1.18.1 | collector suportado; <https://grafana.com/docs/alloy/latest/release-notes/> |
| Caddy | 2.11.4-patched.1 | gateway TLS/mTLS; imagem recompilada em `deploy/caddy` com dependencias Go corrigidas |
| age | 1.3.1 | backup autenticado via biblioteca Go; <https://github.com/FiloSottile/age/releases> |
| Prometheus | 3.13.2 | métricas local; <https://github.com/prometheus/prometheus/releases> |
| Loki | 3.7.6 | logs monolíticos; <https://grafana.com/docs/loki/latest/release-notes/> |
| Tempo | 3.0.3 | traces e metrics-generator; <https://grafana.com/docs/tempo/latest/release-notes/> |
| Pyroscope | 2.2.1 | profiling; <https://github.com/grafana/pyroscope/releases> |
| Blackbox exporter | 0.28.0 | probes; <https://github.com/prometheus/blackbox_exporter/releases> |
| Node exporter | embutido no Alloy 1.18.1 | `prometheus.exporter.unix`; evita uma imagem adicional vulnerável; <https://grafana.com/docs/alloy/latest/reference/components/prometheus/prometheus.exporter.unix/> |
| cAdvisor | embutido no Alloy 1.18.1 | `prometheus.exporter.cadvisor`, perfil privilegiado opt-in; <https://grafana.com/docs/alloy/latest/reference/components/prometheus/prometheus.exporter.cadvisor/> |
| kube-state-metrics | 2.19.1 | objetos Kubernetes; <https://github.com/kubernetes/kube-state-metrics/releases> |
| k6 | 2.2.0 | carga/API; <https://github.com/grafana/k6/releases> |
| Playwright | 1.62.1 | browser E2E; <https://playwright.dev/docs/release-notes> |
| Keycloak | 26.7.1 | OIDC local opcional; <https://www.keycloak.org/docs/latest/release_notes/> |
| Mailpit | 1.30.7 | email local; <https://github.com/axllent/mailpit/releases> |

Compatibilidade foi deliberadamente limitada a amd64 e arm64. Node exporter e
cAdvisor são ativados apenas onde o host permite os mounts necessários.

O Compose usa `mirror.gcr.io` e `public.ecr.aws/docker/library` como mirrors de
distribuição para imagens Docker Hub, preservando nomes upstream e tags exatas.
Isso reduz falhas por rate limit sem alterar a versão do componente.

## Build Alloy corrigido

A release oficial Alloy 1.18.1 tinha findings HIGH corrigíveis. O Compose usa
`deploy/alloy/Dockerfile.patched`, que fixa o commit upstream, Go 1.26.6,
go-git/x-mod, gRPC-Go 1.83.1 e aplica o patch oficial da autorização Moby. A
revisão `sentinel.2` foi criada em 2026-09-03 para absorver o gRPC corrigido;
o scan local e o CI precisam ficar verdes antes de qualquer promoção. Produção ainda exige
publicar o build multiarch por digest, SBOM/provenance, assinatura Cosign e
verificação por admission policy; o digest arm64 local não é promovível.
