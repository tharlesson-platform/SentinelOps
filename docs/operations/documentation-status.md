# Estado de cobertura da documentação

Data da avaliação: 2026-08-24.

## Cobertura executável

| Tema | Estado atual | Evidência/condição |
|---|---|---|
| Plataforma local e mocks | Completo e provado | `make local-demo`; imagens próprias por ID SHA256, API/worker duplicados, failover e storefront → orders → payments correlacionados nos quatro backends |
| Quickstart e Linux faseado | Completo para single-node | bootstrap do zero com download HTTPS+SHA, Systemd/OpenRC; seis famílias no preflight e Docker/Compose instalados no Rocky Minimal |
| Ingestão remota | Completo para piloto seguro | TLS 1.2/1.3, mTLS, SAN SPIFFE tenant/nome, autenticação API-gateway e receivers diretos em loopback |
| Coleta geral Linux | Completo e provado | bundle autocontido sem chave da CA; identidade SPIFFE mTLS; Unix exporter, logs, OTLP local; série real persistida no Prometheus; cAdvisor opt-in |
| Bootstrap APM | Completo para configuração inicial | 11 runtimes; métrica, log e trace provados até Prometheus, Loki e Tempo; dependências são fixadas no repo da aplicação |
| Dashboards de hosts e apps | Completo e provado | 35 provisionados; Linux/APM/Application/Docker especializados; host e aplicações controladas retornam séries vivas |
| API/OpenAPI | Cobertura de todas as rotas | `docs/api/openapi.yaml` v0.2.0 |
| OIDC/RBAC | Implementado, homologação externa pendente | issuer/JWKS; `role_bindings` é autoritativo; validar MFA/grupos no IdP real |
| Isolamento multi-tenant | Implementado no control plane e transporte | filtros explícitos, FORCE RLS em 41 tabelas, role runtime não-superusuária, cert tenant-bound; backends PRD devem habilitar multi-tenancy |
| Gate PromQL/LogQL/TraceQL/SLO | Implementado e provado | prova E2E: 5 PASS; política ausente INCONCLUSIVE; threshold excedido FAIL |
| SSRF | Implementado | http/https absoluto, host allowlist, redirect revalidado e testes negativos |
| Backup/restore | Implementado e provado localmente | age autenticado; 3 bancos + bucket; restore isolado e checksums |
| Upgrade/rollback | Implementado e provado localmente | retenção de imagens, health gate e rollback automático ensaiado |
| Alloy | Build corrigido e scan local verde | v1.18.1 + patches; zero HIGH/CRITICAL; falta publicar/assinar digest multiarch |
| Kubernetes HA | Chart do control plane e gateway completo e renderizado | 26 recursos válidos; réplicas 5/5/3/3, digests, migração, gateway mTLS, probes, PDB, HPA, topology spread fail-closed e NetworkPolicy; backends são externos |
| AWS data plane | Roots DEV/STG/PRD completos, sem apply | RDS/S3/KMS/VPC inputs validáveis; plan/apply/restore exigem conta e aprovação reais |
| Azure/on-premises HA | Arquitetura e gates, sem apply | serviços gerenciados/cluster/DR precisam de plano e evidência do alvo |
| CI remoto/supply chain | Repositório público configurado; promoção fail-closed | `tharlesson-platform/SentinelOps`; CI no SHA, branch protection, registry, assinatura e admission continuam gates obrigatórios de cada promoção |
| Faro/RUM | Parcial | kit React exige endpoint Faro HTTPS; receiver produtivo não faz parte do stack local |

## O que “funcional” significa

O perfil Linux single-node pode ser entregue, instalado, monitorar hosts e
receber APM de forma segura. Ele continua sem HA por definição. O perfil
Kubernetes renderiza somente API/worker/web e depende de PostgreSQL, Temporal,
Mimir/Prometheus, Loki, Tempo e IdP operados externamente.

No perfil local, `make prove-local` comprova a cadeia de três aplicações desde
a chamada sintética, coleta e processamento no Alloy até Prometheus, Loki,
Tempo e Pyroscope. A mesma execução cadastra os mocks no control plane e prova
os estados PASS, INCONCLUSIVE e FAIL dos gates. Isso é aceite local reproduzível,
não evidência de produção externa.

O perfil local padrão executa duas réplicas de API/worker e uma borda com DNS
dinâmico. `make prove-ha` interrompe uma API, exige estabilidade e restaura as
réplicas; isso cobre falha de processo, não a perda do host. O guard de Compose
recusa `SENTINEL_ENV=production`, impedindo que esse perfil seja promovido por
engano.

Não declare produção apenas com containers `running`. Faltam evidências do alvo
real: publicação de imagens por digest e assinatura, checks remotos no SHA,
OIDC/MFA homologado, backends multi-tenant/HA, restore regional, NetworkPolicy
compatível com o CNI e rollout/rollback no cluster. Esses são gates externos,
não lacunas escondidas da documentação.
