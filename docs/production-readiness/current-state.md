# Estado atual de production readiness

Auditoria iniciada em 2026-09-14, sobre a branch `fix/linux-vm-bootstrap` no
commit base `f9675f8`, preservando a árvore de trabalho pré-existente. Este
documento separa presença de código, evidência local, evidência de integração e
homologação em alvo real. `PRODUCTION READY` só pode ser usado após os gates
externos; no estado atual, a plataforma como um todo não está aprovada para
produção.

## Escopo e evidências executadas

- `git status --short --branch`, `git diff --stat` e `git diff --check` para
  delimitar a árvore suja; havia 90 arquivos modificados e muitos arquivos
  novos antes desta iteração.
- Inventário de 359 arquivos versionáveis, código Go, React, migrations,
  Compose, Helm, Terraform, collectors, dashboards, workflows e documentação.
- `make harness-doctor`: Docker 29.8.0 e Compose 5.5.1 disponíveis.
- `make harness-validate-specs`: 20 specs válidas; todas permaneciam
  `IN_PROGRESS` e nenhuma possuía evidência para `DONE`.
- Primeira execução de `make harness-check`: falhou em
  `TestEmptyNotificationWebhookObjectDoesNotNeedAllowedHosts`; o teste foi
  isolado da autenticação local. O rerun completo após as correções passou:
  todos os packages Go com race detector, 6 testes web, build TypeScript/Vite,
  contratos Windows/network/cloud/Kubernetes/PostgreSQL/APM e backup/restore.
- Primeira execução de `make up`: o Compose passou a renderizar após reconciliar
  defaults não secretos, mas o pull de MinIO falhou porque o digest oficial não
  existia em `mirror.gcr.io`. O mesmo tag e digest foram verificados em
  `quay.io/minio/minio` e `quay.io/minio/mc`; o registry foi corrigido sem mudar
  a versão.
- O segundo `make up` iniciou o stack local com duas APIs, dois workers e os
  backends saudáveis. O profile `demo` iniciou a cadeia API → Orders → Payments,
  host Linux controlado e gerador de tráfego; o seed foi corrigido para o
  contrato tipado de assertions.
- O reviewer detectou e removeu uma mistura de provenance: o Prometheus do
  Compose voltou a rotular `environment=local-demo` e o blackbox passou a mirar
  somente `demo-api`; o laboratório não consulta nem rotula alvo externo como
  produção.
- A prova local integrada terminou em `PASS` para cadeia, métricas, logs, traces,
  profiles e quality gate padrão; confirmou ainda `INCONCLUSIVE` para política
  ausente e `FAIL` para threshold violado. Evidência:
  `artifacts/local-e2e/20260914T125652Z.json`.
- As APIs nativas retornaram 1 host, 3 serviços APM, 200 logs e 50 traces na
  janela consultada; Docker retornou lista vazia com estado `no_data`, pois o
  cAdvisor privilegiado não foi habilitado sem aprovação explícita.
- `make harness-integration-compose` passou os testes PostgreSQL de database,
  events e HTTP API com runtime non-superuser. `make harness-e2e` passou 1 teste
  Playwright e k6 concluiu 150 iterações/450 checks, zero falhas e p95 2,62 ms.
- Helm lint/template, 27 recursos via kubeconform e Caddy validate passaram. A
  prova mTLS em cluster e a negação com certificado real continuam abertas.
- HA local passou com duas APIs, dois workers, interrupção de uma réplica, 50
  probes e restauração em 12 s; evidência em
  `artifacts/resilience/ha-20260914T124805Z.json`.
- Em 2026-09-14, a assinatura Azure foi reconsultada e quatro VMs Linux de
  aplicação foram inspecionadas. `tqi-platform` e `easy-vm` possuem collectors
  Docker completos e saudáveis; `gitlab-vm` foi preparado, mas permanece sem
  ativação até receber certificado mTLS exclusivo da CA de produção; o edge não
  possui Docker e não alcança o gateway. A evidência detalhada está em
  `docs/production-readiness/azure-docker-rollout-2026-09-14.md`.
- Consultas read-only no Data Plane confirmaram `up=1` com freshness inferior a
  13 s para `tqi-platform`/`easy-vm`, 24/23 containers, 65/28 séries Beyla e
  2.119/22.120 eventos Docker nos últimos 5 minutos. Também provaram uma
  divergência crítica: Prometheus, Loki e Web estão acessíveis diretamente na
  rede privada, em vez de ficarem restritos ao loopback/gateway.

## Inventário funcional

| Área | Estado | Evidência | Problema | Ação |
|---|---|---|---|---|
| Arquitetura Control Plane/Data Plane | DONE | `docs/architecture/overview.md`, ADR-0001 e código Go/React/PostgreSQL/Temporal | Diagramas não provam isolamento no alvo | Revalidar mTLS, tenancy e NetworkPolicy em cluster autorizado |
| API Go | PARTIAL | Rotas de catálogo, agentes, release, validação, incidentes, lifecycle e observabilidade; testes focados PASS | Cobertura de contrato não inclui todas as rotas e paginação não é uniforme | Expandir contract/integration tests e paginação |
| Frontend React | PARTIAL | Build PASS; Catálogo, Test Studio, Delivery, Agents, Governance e primeira experiência nativa de observabilidade | ReleaseView continua sem consulta; perfis ainda não têm flamegraph nativo; i18n é parcial | Concluir rotas funcionais, i18n e E2E por jornada |
| Banco PostgreSQL | PARTIAL | 12 migrations; integração Compose de database/events/API passou com runtime non-superuser | Upgrade/rollback N-1 e DR continuam sem evidência | Ensaiar upgrade e restore em staging isolado |
| RLS multi-tenant | PARTIAL | FORCE RLS e isolamento entre dois tenants passaram no PostgreSQL Compose | O banco produtivo e o pooler não foram homologados | Repetir no caminho produtivo completo |
| Migrations | PARTIAL | Lock/checksum e arquivos `000001`–`000012` | Upgrade/rollback real não revalidado nesta execução | Testar banco vazio, upgrade N-1 e rollback compatível |
| Dockerfiles | DONE | Imagens próprias constroem com usuários não-root e digests de base | Build local não é promoção assinada | Executar scan/SBOM/signature no SHA promovido |
| Docker Compose local | DONE | Render, build, start, seed, pipeline local, HA, Playwright e k6 passaram | É laboratório local, não implantação produtiva | Manter determinístico e separado dos valores de produção |
| Bootstrap `.env` | DONE | `scripts/bootstrap.sh` reconcilia defaults sem sobrescrever secrets; Compose renderizou | Migração cobre apenas defaults conhecidos | Manter teste de upgrade de configuração |
| Helm Chart | PARTIAL | Lint/template PASS; 27 recursos kubeconform PASS; Caddy validate PASS | API→gateway mTLS e políticas não foram homologados em cluster | Dry-run e teste positivo/negativo no cluster autorizado |
| Terraform | NEEDS TESTING | Módulos para PostgreSQL e object storage, ambientes dev/stg/prod | Sem fmt/validate/plan atual e sem backend real informado | Rodar fmt/validate; plano somente em conta autorizada |
| CI GitHub Actions | PARTIAL | Workflows CI/release presentes, imagens fixadas | Estado remoto e SHA promovido não consultados | Revalidar checks remotos, assinatura e approvals |
| GitLab/Jenkins/Azure DevOps/CodePipeline | NEEDS TESTING | Exemplos em `examples/integrations/` | Exemplos não são integrações homologadas | Executar webhook/gate em cada plataforma autorizada |
| Autenticação local | DONE | bcrypt, JWT curto, issuer/audience/exp; testes PASS | Explicitamente proibida em produção | Manter somente para development/local/test |
| OIDC | PARTIAL | Verificação issuer/audience/scope e RBAC persistido | Entra ID/Keycloak reais, MFA e logout não homologados | Testar discovery, claims, revogação e sessão no IdP real |
| RBAC backend | PARTIAL | `auth.Can`, `require` e novo `telemetry:read`; testes de auth PASS | Matriz completa de rotas/roles não está testada | Adicionar testes table-driven de deny/allow por rota |
| Audit log | PARTIAL | Ações mutáveis relevantes gravam audit events | Cobertura completa de login/logout/dashboard/SLO/token não comprovada | Inventariar e testar todos os eventos obrigatórios |
| Secrets | PARTIAL | `.env` ignorado, modo 0600, secrets em Helm/Compose | Gestão/rotação externa e secret scanning atual não provados | Integrar secret manager e gates de vazamento |
| Quotas/rate limiting | DONE | Quota global e tenant-scoped com 429/Retry-After; testes de config | Capacidade real não dimensionada | Load test e ajuste por ambiente |
| Gateway de ingestão mTLS | PARTIAL | Caddy allowlisted e SPIFFE por organização | Certificados/CA/DNS e 421 precisam revalidação no alvo | Provar cert válido, inválido, replay e isolamento por tenant |
| Gateway de consulta | PARTIAL | Prometheus instant/range, Loki e Tempo allowlisted; SANs separados para worker/API | mTLS e upstream CA ainda não executados no cluster | Homologar API/worker e bloquear acesso direto aos backends |
| Cliente nativo de telemetria | DONE | Limite 8 MiB, timeout, retry, tenancy e parsers com testes; Prometheus/Loki/Tempo locais consultados pela API | Gateway mTLS externo ainda pendente | Exercitar falhas parciais e gateway no cluster |
| Operations Overview nativa | PARTIAL | API consulta hosts/containers/services; UI exibe `Sem dados` em null | SLO, alerts, delivery e synthetic ainda não compõem o overview | Integrar fontes restantes sem inventar saúde |
| Dashboard/lista de Hosts nativa | PARTIAL | Descoberta por `node_uname_info`/`up`, CPU/memória/disco e drill-down | Processos, conexões TCP, OOM e metadata cloud ainda incompletos | Completar queries, recording rules e testes com collector real |
| Host Details nativo | PARTIAL | Séries de CPU por modo, load, memória, swap, filesystems, I/O e rede | Sem tabela de filesystem/inodes e top processes | Implementar modelos dedicados e estados por capacidade |
| Dashboard Docker nativa | PARTIAL | cAdvisor: identidade, CPU, memória, limite, restarts e drill-down | Status/health/exit code não existem em cAdvisor puro | Adicionar inventário Docker read-only separado e correlacionar |
| Container Details nativo | PARTIAL | CPU, throttling, memória, network e filesystem em range | Lifecycle/health/OOM precisa fonte adicional | Integrar inventory/event collector e testes E2E |
| Logs por host | PARTIAL | API LogQL allowlisted e UI com janela, severidade e busca | Não validado contra Loki vivo nesta execução | Provar host real e comportamento Loki indisponível |
| Logs por container | PARTIAL | Collector extrai `container_id` do filename sem docker.sock; UI preserva contexto | Deploy dos collectors antigos ainda não possui o novo label; nome exige join com cAdvisor | Fazer rollout versionado e confirmar cardinalidade/freshness |
| Logs por service | PARTIAL | Filtro `service_name` retornou logs OTLP reais do demo pela API nativa | Docker JSON não conhece service.name por si só | Enrichment controlado por inventário, sem labels de alta cardinalidade |
| Live Tail | NOT IMPLEMENTED | Não há endpoint SSE/WebSocket de tail na UI nativa | Requisito final 8 está aberto | Implementar proxy Loki tail com cancelamento, limites e backpressure |
| Metrics Explorer | PARTIAL | Visões allowlisted e charts nativos; range até 30 dias | Builder amigável e modo PromQL avançado autorizado ainda faltam | Criar AST/query builder e permissão separada para PromQL |
| APM | PARTIAL | RED, errors, p95/p99 retornaram 3 serviços do demo pela API/UI nativa | Endpoints/topology e app representativa ainda faltam | Normalizar semantic conventions e endpoint analysis |
| Distributed Tracing | PARTIAL | Busca TraceQL por service retornou traces distribuídos reais; UI lista e preserva contexto | Waterfall/span details e log correlation por trace_id faltam | Implementar detalhe de trace e surrounding logs |
| Profiles/Pyroscope | PARTIAL | Scrape pprof local produziu profiles dos 3 serviços e flamegraph com amostras | API nativa não possui rota segura nem render de flamegraph | Definir rota allowlisted, modelo e flamegraph nativo |
| Grafana dashboards | PARTIAL | 35+ JSON gerenciados e prova estrutural existente | Vários dashboards vieram de geração comum e não equivalem à UI nativa; alvo atual não revalidado | Manter para análise avançada e provar queries/freshness por painel |
| Prometheus/Mimir | PARTIAL | `up=1` e freshness <13 s para dois hosts Azure; remote-write failures total 0 | `0.0.0.0:9090` está acessível sem gateway na rede privada; Mimir/HA não homologado | Restringir bind/firewall, testar remote_write, ruler, retenção e tenancy |
| Loki | PARTIAL | Consultas retornaram 2.119/22.120 eventos Docker em 5 min e `/ready` recuperou 200 | Porta 3100 diretamente acessível; retenção, HA e restore não homologados | Restringir bind/firewall e provar perdas, retries, tail e restore |
| Tempo | PARTIAL | OTLP, TraceQL e service graph em dashboards | Busca/detalhe native e storage distribuído incompletos | Provar trace→log→deployment e retenção |
| Pyroscope | PARTIAL | Compose e ingestão local existentes | Experiência nativa e HA não implementadas | Fechar API/render e resilience/retention |
| Alertmanager/alerting | PARTIAL | Rules e provisioning Grafana presentes | Control Plane não lista firing/pending/resolved do Alertmanager | Implementar cliente/normalização e incident context |
| Alloy | PARTIAL | Pipelines de metrics/logs/traces e redaction | Rollout/freshness em toda a frota não comprovados | Versionar collector, self-metrics e alertas de drop |
| Linux Collector | PARTIAL | `tqi-platform` e `easy-vm` ativos/ready; `gitlab-vm` passou preflight/runtime; edge revalidado sem Docker | GitLab aguarda certificado exclusivo; edge não alcança o gateway | Emitir cert no host da CA, ativar GitLab e corrigir rota do edge |
| Docker Collector | PARTIAL | Dois hosts com Alloy/logs/cAdvisor/Beyla, zero restarts; terceiro host preparado com imagens e hashes conferidos; `easy-vm` com rotação validada | `gitlab-vm` ainda não envia telemetria; `tqi-platform` está em 81% sem rotação; cAdvisor permanece privilegiado | Ativar GitLab com cert próprio, tratar disco/retenção do TQI e medir overhead |
| Kubernetes Collector | PARTIAL | App de inventory, RBAC render e harness check | Nenhum cluster atual homologado | Executar com ClusterRole read-only e validar KSM/cAdvisor |
| ECS/AWS | PARTIAL | Cloud inventory e dashboards/configs presentes | Sem conta/task real nesta auditoria | Assumir role read-only e provar ECS/CloudWatch freshness |
| Azure | PARTIAL | Inventário live de 2026-09-14, dois collectors completos, GitLab recuperado de disco cheio e pré-configurado; retenção Docker ativa em Easy/GitLab | Certificado GitLab, retenção/disco do TQI e rota do edge pendentes; produção geral ainda não aprovada | Fechar identidade mTLS, disco/retenção, validar Prometheus/Loki/Tempo e reexecutar review |
| VMware | PARTIAL | Exporter govmomi e testes; histórico de counters reais | Evidência não foi atualizada nesta execução | Revalidar vCenter/ESXi, latency samples e cardinalidade |
| Windows | PARTIAL | Installer PowerShell e check estático | Sem host Windows/AD homologado | Testar assinatura, serviço, perf counters e logs reais |
| Network/FortiGate | PARTIAL | Renderer/installer SNMP/syslog | Sem equipamento real e credenciais autorizadas | Validar ACL, SNMPv3, syslog TLS e dashboard |
| PostgreSQL exporter | PARTIAL | Exporter próprio e harness check | Uma instância local não prova variedade/least privilege | Testar usuário read-only e queries sob carga |
| Synthetic HTTP/TCP/DNS/TLS | PARTIAL | Agent anuncia capacidades; HTTP runner possui SSRF fail-closed | TCP/DNS/TLS end-to-end e fleet distribuída não comprovados | Implementar/validar runners reais por localidade |
| Browser/k6 synthetic | NOT IMPLEMENTED | API retorna `unsupported_capability` explicitamente | Não existe runner distribuído nem artifact lifecycle completo | Implementar runners isolados e storage em S3/MinIO |
| Test Studio | PARTIAL | Formulário HTTP persiste cenário real | cURL/OpenAPI/Postman/HAR/YAML/k6/Playwright faltam | Implementar importadores com validação e preview |
| Release Validation | PARTIAL | Temporal workflow e gates PromQL/LogQL/SLO/TraceQL | UI de release está desconectada; baseline/stabilization amplo incompleto | Integrar frontend, release timeline e adapters reais |
| Correlation Engine | NOT IMPLEMENTED | Há gates e incident lifecycle, mas não há engine de evidência multi-sinal | Não classifica highly/probable/possible/inconclusive | Criar modelo causal conservador e testes de contraprova |
| SLO/error budget | PARTIAL | Gates SLO e dashboard Grafana | CRUD/políticas e UI nativa completas não existem | Implementar persistência, multi-window burn rate e alertas |
| Incident timeline | PARTIAL | Incidentes, deliveries, escalations e dashboard existem | Timeline unificada nativa não foi implementada | Normalizar eventos de deploy/alert/log/trace/synthetic |
| Capacity Agent | PARTIAL | Dashboard/Spec e bases de forecast existem | Agente estatístico robusto e validação 7/30/90d não comprovados | Implementar MAD/EWMA/regression com intervalos de confiança |
| Observability Assistant | PARTIAL | Guardrails consultivos fail-closed e evaluator | Provider/data plane real, evidências e UI não homologados | Manter disabled até avaliação e ACL end-to-end |
| `sentinelctl` | PARTIAL | Login, catálogo, scenarios, releases, validations e agents; testes | Dashboard/SLO/config e códigos não batem integralmente com o pedido | Completar comandos e contract tests |
| Self-observability | PARTIAL | API metrics, structured logs e OTel em componentes | Dropped telemetry, queue depth e dashboards nativos incompletos | Definir SLO da própria plataforma e alertas |
| Retenção/object storage | PARTIAL | MinIO/S3 e documentação/configs existem | Políticas por ambiente e restore de artifacts não revalidados | Executar lifecycle/retention e restore |
| Backup/restore | PARTIAL | `backupcrypt`, scripts e testes unitários presentes | Restore completo e DR no alvo não foram executados | Provar RPO/RTO, integridade, chaves e rollback |
| Performance/capacity | NEEDS TESTING | k6 smoke e alguns harnesses existem | Sem baseline de ingestão/query/concurrent users | Executar teste controlado e registrar budgets |
| Dark/light | DONE | Tema persistido e build web PASS | QA visual completa ainda necessária | Capturar telas responsivas nos dois temas |
| pt-BR/en-US | PARTIAL | i18next configurado e seletor disponível | Muitas strings novas e antigas continuam hardcoded em pt-BR | Extrair todas as mensagens para catálogo |
| Timezone/UTC | PARTIAL | `America/Sao_Paulo` em runtime e timestamps de API em UTC | Preferência por usuário/organização não existe | Persistir timezone e testar DST/conversão |
| linux/amd64 e linux/arm64 | PARTIAL | Bases multiarch e builds locais arm64 | Build/scan amd64 atual não executado | CI matrix multiarch com imagens promovidas |
| Licenças | PARTIAL | `LICENSE` e versões documentadas | Inventário transitivo/SBOM/licenças ainda incompleto | Gerar SBOM e revisar incompatibilidades Apache-2.0 |
| Runbooks | PARTIAL | Runbooks de APM/Docker/resposta operacional | Lista mínima solicitada não está completa | Criar runbooks por backend/gate com sintomas e rollback |
| Produção externa | BROKEN | Gate histórico `REJECTED`; não há evidência fresca suficiente | IdP, collectors, CI promovido, rollback e DR continuam abertos | Manter bloqueado até checklist e revisão SRE independente |

## Decisão atual

O SentinelOps possui uma base executável relevante, mas não é production ready.
A entrega desta iteração só será marcada como concluída por capacidade quando o
respectivo teste listado no plano estiver verde. Integrações externas sem alvo
autorizado permanecem `NEEDS TESTING` ou `PARTIAL`, nunca `DONE`.
