# Inventário de licenças de terceiros

Data: 2026-08-17. Este arquivo registra dependências diretas e componentes do
perfil local. O SBOM CycloneDX/SPDX da release é a fonte completa para
dependências transitivas e digests promovidos.

O código próprio do SentinelOps é distribuído sob a licença MIT. As licenças
abaixo permanecem aplicáveis aos componentes e às dependências de terceiros.

| Componente | Licença principal | Observação |
|---|---|---|
| Go, PostgreSQL | BSD/PostgreSQL | permissivas |
| React, TypeScript, Vite, Playwright | MIT / Apache-2.0 | frontend e E2E |
| OpenTelemetry, Prometheus, Alloy, Tempo, Pyroscope, k6, Blackbox | Apache-2.0 | telemetria/testes |
| Grafana OSS, Loki | AGPL-3.0 | serviços separados; revisar obrigações ao distribuir modificações |
| Temporal Server/UI | MIT | workflow engine |
| MinIO | AGPL-3.0 | serviço local separado; revisar oferta de rede/modificações |
| Keycloak | Apache-2.0 | OIDC |
| Mailpit | MIT | email local |
| Go pgx, jwt, uuid, yaml, Prometheus client | MIT/BSD/Apache-2.0 | bibliotecas diretas |
| NGINX, Alpine Linux | BSD-2-Clause e múltiplas | imagem web/runtime |

Antes de distribuir uma release:

1. gere SBOM por imagem e fonte;
2. associe o digest da imagem ao SBOM e assinatura;
3. execute policy scan para licenças proibidas;
4. publique notices exigidos e preserve copyright;
5. obtenha revisão jurídica para AGPL e plugins Grafana.
