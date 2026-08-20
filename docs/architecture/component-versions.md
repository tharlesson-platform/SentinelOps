# Versões dos componentes

Verificadas em 2026-08-20 nas páginas oficiais de releases e registries. Todas
as imagens do Compose usam tags exatas; para produção, o pipeline deve promover
por digest após scan e assinatura.

| Componente | Versão | Motivo / fonte oficial |
|---|---:|---|
| Go | 1.26.6 | toolchain estável, multiarch; <https://go.dev/dl/> |
| Node.js | 26.7.0 | build do frontend; <https://nodejs.org/en/download> |
| React | 19.2.8 | UI; <https://www.npmjs.com/package/react> |
| TypeScript | 7.0.2 | tipagem; <https://www.npmjs.com/package/typescript> |
| Vite | 8.2.1 | build; <https://www.npmjs.com/package/vite> |
| PostgreSQL | 18.3 | banco relacional; <https://www.postgresql.org/docs/release/> |
| Temporal | 1.29.7 | versão mais recente publicada na imagem oficial `auto-setup`; o SDK 1.47.0 mantém compatibilidade; <https://hub.docker.com/r/temporalio/auto-setup/tags> |
| Temporal UI | 2.53.3 | inspeção local; <https://github.com/temporalio/ui/releases> |
| MinIO | RELEASE.2025-09-07T16-13-09Z | versão mais recente publicada na imagem oficial; <https://hub.docker.com/r/minio/minio/tags> |
| Grafana | 13.1.3 | visualização e alertas; <https://github.com/grafana/grafana/releases> |
| Grafana Alloy | 1.18.1 | collector suportado; <https://grafana.com/docs/alloy/latest/release-notes/> |
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

## Exceção de segurança atual

Em 2026-08-20, Trivy encontrou 12 vulnerabilidades HIGH corrigíveis no binário
da release oficial Alloy 1.18.1. Como não havia release upstream posterior, a
versão permanece somente para laboratório/piloto. Produção exige release
corrigida ou build interno reproduzível, escaneado, assinado e revisado.
