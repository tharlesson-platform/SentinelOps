# Matriz de capacidades e evidência

Estados: `ausente`, `parcial`, `implementado`, `validado-local`,
`validado-alvo`, `operacional`. Não promover por README, container `1/1` ou
HTTP 200.

| Capacidade | Código | Teste local | Alvo/produção |
|---|---|---|---|
| Harness/versionamento | implementado | validate-specs/check | ausente |
| UI sem medição fictícia | implementado | build web | ausente |
| Inventário de ativos estáveis | implementado | API/CLI, migration e RLS PostgreSQL | descoberta TQI pendente |
| Sintético HTTP seguro | implementado | unitário + PostgreSQL isolado | ausente |
| Claim multi-réplica | validado-local | concorrência PostgreSQL | ausente |
| Gates de telemetria por janela/freshness | implementado | unitário com stale/partial/NaN/cobertura TraceQL | ausente |
| Consulta tenant-aware aos backends | implementado | worker exige gateway mTLS; Caddy/Helm renderizados | homologação de backends multi-tenant pendente |
| Webhook inbox/outbox/SSE | validado-local | PostgreSQL isolado: HMAC → outbox → worker → replay SSE | homologação de integração externa pendente |
| Browser/k6 distribuído | validado-local; execução distribuída continua ausente e a API a rejeita | Playwright: login/dashboard/API sem erros de console; k6: 444 checks, 0 falhas, p95 5,17 ms | runner distribuído, alvo autorizado e homologação pendentes |
| Frota, inventário por agente e revogação | validado-local | PostgreSQL/RLS, API e CLI | gateway mTLS e agente-alvo pendentes |
| Linux/Docker Azure | parcial | validação histórica local | gateway/SSH bloqueados |
| Inventário Azure read-only | parcial | Resource Graph paginado, subscription exata, snapshot atômico e testes Go | subscription federada, 429/expiração e comparação nativa pendentes |
| Inventário AWS read-only | parcial | AWS Config paginado, account/região exatas e recusa de recorder incompleto | conta sandbox, federação/expiração e comparação nativa pendentes |
| Inventário Kubernetes read-only | parcial | namespaces explícitos, UID estável, RBAC negativo e snapshot atômico | cluster isolado, ServiceAccount projetada e comparação nativa pendentes |
| Métricas PostgreSQL read-only | parcial | conexões, waits, locks/deadlocks, TLS obrigatório, scrape/rules PromQL validados | banco homologado, role pg_monitor e entrega externa de alerta pendentes |
| APM e correlação OTLP | parcial | kits por runtime, W3C, redaction Alloy e negação de endpoint inseguro | aplicação TQI, três serviços reais, PII e baseline CPU/p95 pendentes |
| RUM/Web Vitals seguro | parcial | kit React com HTTPS, sampling explícito, URL redigida e Session Replay desligado | receiver com CORS/rate limit, regressão correlacionada e revisão LGPD pendentes |
| Windows/AD e serviços Microsoft | parcial | templates Alloy, instalador com SHA/ACL/bookmarks e contrato estático | host Windows, política AD, alerta e jornada de autenticação pendentes |
| Rede/FortiGate | parcial | SNMPv3 authPriv, allowlist IP exata, syslog UDP/TCP opcional com Loki mTLS e Compose/Alloy validados | ACL/VLAN, emissor, equipamento, traps e alertas de homologação pendentes |
| VMware | parcial | exporter e validação de endpoint/thumbprint/permissão de segredo; evidência histórica de 8 séries | vCenter read-only, counters selecionados e condição degradada pendentes |
| IdP/RBAC corporativo | implementado | contrato issuer/audience/scope + RBAC autoritativo | homologação IdP/MFA pendente |
| Incidentes/ACK/outbox | implementado | testes Go locais | ausente |
| Entrega webhook de notificação | validado-local | PostgreSQL e endpoint isolado | ausente |
| Escalonamento de notificação | validado-local | PostgreSQL, entrega e cancelamento por ACK | canal externo homologado pendente |
| Teams/e-mail | parcial | não executado | ausente |
| MCP read-only de inventário | implementado | SDK MCP por stdio, validação de schema e API/RBAC | token de homologação, allowlist do cliente e gateway mTLS pendentes |
| Guardrail de IA/RCA pré-provider | parcial | 30 cenários sanitizados, abstenção, tenant/proveniência/orçamento | gateway/modelo, auditoria e homologação pendentes |
| Quotas de requisição e consulta por tenant | validado-local | janelas PostgreSQL atômicas com RLS, 429/Retry-After, métrica/alerta sem cardinalidade; quota adicional `catalog-query` cobre API/MCP de inventário; teste Compose efêmero passou com migration/runtime separados | quotas de sinal, bytes, séries, storage, IA e carga multi-réplica pendentes |
| Solicitação de exportação/exclusão | parcial | API/UI, RLS, auditoria transacional, quatro olhos e erasure de identidade do control plane em PostgreSQL isolado | exportação, telemetria/objetos, política de retenção e executores por backend pendentes |
| Backup/restore isolado | parcial | contrato autenticado, alvo Compose inédito, checksums e evidência sanitizada; restore real pendente | ausente |
| DR completo (PITR, CA/segredos e região) | ausente | ausente | ausente |
