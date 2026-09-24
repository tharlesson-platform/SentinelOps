## SRE REVIEW REPORT

### 1. Resumo da entrega

Revisão independente da renovação local do SentinelOps e do portal de observabilidade, em 18/09/2026. Escopo: seleção de servidores/containers, métricas, logs, traces, perfis, navegação, identidade TQI provisória, autenticação existente e contratos de correlação. Não inclui gestão de usuários, publicação, infraestrutura nova ou aprovação de produção.

Foram inspecionados App.tsx, Observability.tsx, navigation.ts, api.ts, i18n.ts, temas/logotipos, portal/index.html, observability.go, testes e a inclusão de host_name nos dois perfis Alloy. O diretório private-ingress e alterações anteriores de App/API/Vite já existiam; nginx.conf não foi tratado como alteração desta entrega. Nenhum arquivo de implementação foi alterado pelo revisor.

Validação executada pelo revisor:

- `git diff --check`: passou.
- `npm test` em apps/web: 15 testes passaram na revisão final.
- `VITE_BASE_PATH=/sentinelops/ npm run build` em apps/web: TypeScript e Vite passaram após restauração do import ShieldCheck, incluindo a base do ingresso.
- `/tmp/sentinel-toolchain/go/bin/go test ./internal/httpapi ./internal/telemetryquery`: passou, incluindo testes novos de consulta global de logs.
- Playwright local com fixtures, `playwright.local.config.ts`: 18/18 passaram em 7,5 segundos. Resultado separado em `/tmp/sentinel-sre-playwright`.
- Evidência final comunicada pelo implementador: os mesmos 18/18 passaram sobre o bundle `/sentinelops/`, servido por preview local na porta 4319, em 5,1 segundos após estabilização do artefato. O teste isolado do portal também passou após usar o `baseURL` do projeto, removendo a suposição de localhost no teste. Esta rodada não foi reexecutada pelo revisor.
- Reprodução independente da navegação métricas → logs → Voltar com resposta atrasada: scrollY 1500 → 1500.
- Inspeção visual dos artefatos locais de visão geral, portal e mobile claro; sem estouro horizontal aparente. As imagens usam fixtures, não telemetria de produção.

Os testes cobrem homônimos em servidores distintos, escolha explícita, seleção sem filtragem implícita, A lento → B, URL/reload, atualização da tela ativa, erro/recuperação, estados de fonte, correlação host_name, busca controlada de logs, encaminhamento Grafana, teclado, temas, mobile, portal, credenciais inválidas, rolagem e fallback do botão Voltar. A documentação `docs/renovacao-interface-tqi-2026-09-18.md` foi lida e descreve contratos, reprodução, limitações e condições de rollout/rollback.

Achados corrigidos durante a revisão: consulta global de logs que excluía streams sem service_name; propagação das duas consultas para o Grafana; falha de overview silenciosa; erro de credenciais confundido com sessão expirada; contraste do botão primário; import que impedia build; fixture Go com client inválido; restauração de rolagem antes dos gráficos. A restauração atual também cancela em nova navegação/interação e tem janela de 20 segundos. O ambiente de origem APM agora é acompanhado de aviso explícito quando não aplicado aos traces.

Revisão adicional: o envelhecimento do SourceBanner foi corrigido com relógio local de 30 segundos, sem novas consultas de telemetria. O intervalo é removido ao desmontar ou trocar fetchedAt, e a classe de fonte indisponível permanece preservada. O teste adicional inspecionado cobre envelhecimento sem requisições, atualização explícita e limpeza do relógio. Evidência final comunicada pelo implementador após esse delta: build/typecheck com base `/sentinelops/` aprovado, 15/15 Vitest, 19/19 Playwright sobre o bundle estável na porta 4319 em 5,5 segundos e `git diff --check` limpo. O revisor inspecionou o delta sem reconstruir o bundle ou repetir essa rodada. O antigo achado de envelhecimento está resolvido.

### 2. Status

- APPROVED WITH CHANGES

Decisão restrita à entrega local revisável. As mudanças restantes são melhorias não bloqueantes e homologação das integrações antes de qualquer publicação autorizada.

### 3. Score final

| Categoria | Score |
|---|---:|
| Segurança | 90 |
| Confiabilidade | 88 |
| Operação | 89 |
| Observabilidade | 88 |
| Custo | 94 |
| CI/CD | 82 |
| Manutenibilidade | 85 |

Os scores avaliam este diff local; não medem maturidade nem disponibilidade da plataforma publicada.

### 4. Riscos encontrados

- **MÉDIO — Integrações reais ainda não homologadas.** Arquivos: `apps/web/src/App.tsx`, trecho `oidcManager`/callback; `apps/web/src/Observability.tsx`, `exploreURL`; `deploy/agents/linux/config.alloy` e `config-cadvisor.alloy`, regras host_name. Problema: fixtures comprovam contrato e navegação local, mas não a sessão real do IdP/Grafana nem os labels dos collectors publicados. Impacto: hosts existentes podem continuar sem correlação e links externos dependerão da configuração real. Correção recomendada: em etapa separada autorizada, validar callback OIDC sob `/sentinelops/`, permissões/sessão, UIDs e consultas Grafana, labels recebidos e rollback do collector. A UI bloqueia correlação ausente e mantém métricas disponíveis; não inventa identidade.
- **MÉDIO — Ambiente APM não restringe traces.** Arquivo: `apps/web/src/Observability.tsx`, componentes Scope/APM e aviso `serviceEnvironment`; `internal/httpapi/observability.go`, filtros de `searchObservedTraces`. Problema: investigação usa serviço, host e container, sem filtro de ambiente. Impacto: serviços homônimos em ambientes distintos podem aparecer juntos quando os atributos de host não distinguem os ambientes. Correção recomendada: evoluir o contrato de atributos OpenTelemetry e filtro real de ambiente. O aviso atual torna a limitação explícita e é aceitável nesta entrega local.
- **BAIXO — Referência visual final pendente.** Arquivo: `apps/web/src/tqi-theme.css`, comentário/tokens TQI, e portal. Problema: TQI Bid não foi inspecionado; a referência usada é o ativo TQI de Orgflow. Impacto: identidade pode precisar de ajuste posterior. Correção recomendada: comparar com a referência quando disponível, alterando tokens/ativos centralizados, sem afirmar aprovação de marca.

### 5. Bloqueadores

Nenhum bloqueador de código identificado para a entrega local revisável após as correções e validações acima.

Publicação continua fora da autorização desta tarefa. Antes de deploy, faltam homologação autenticada das integrações, conferência da revisão/artefatos que serão publicados, validação de Alloy e plano de rollout/rollback aprovado. Esta revisão não substitui esses gates nem reabre acesso de produção.

### 6. Melhorias recomendadas

- Evoluir filtro de ambiente em logs/traces somente com atributo realmente emitido e testado.
- Manter teste autenticado de OIDC e encaminhamentos Grafana em ambiente de homologação; o teste atual valida caminhos/URLs e fixtures.
- Validar os dois perfis Alloy com a versão efetivamente usada antes de distribuir os novos labels.
- A busca global de logs cobre streams com service_name ou host_name. Fontes que não emitam nenhum desses labels precisam de contrato explícito antes de serem apresentadas como cobertas.

### 7. Checklist SRE

| Item | Status |
|---|---|
| Sem secrets hardcoded | OK — nenhum segredo novo identificado no diff revisado |
| Least privilege aplicado | OK — não houve expansão de permissões nem bypass de autorização |
| Logs adequados | OK — escopo explícito, limite, ordenação global, falha parcial e recuperação |
| Métricas definidas | OK — entidade, fonte, período, unidade, séries e lacunas explícitos |
| Health checks definidos | NOK — execução real dos health checks não faz parte da evidência desta revisão |
| Rollback previsto | NOK — não houve ensaio de rollout/rollback desta versão; obrigatório antes de deploy |
| Backup considerado | OK — sem migração de dados ou alteração de armazenamento nesta entrega local |
| Ambientes segregados | OK — testes usam fixtures locais; nenhuma chamada/alteração de produção executada pelo revisor |
| Pipeline com aprovação | NOK — nenhum pipeline remoto ou aprovação de publicação verificado |
| Documentação suficiente | OK — limitações e condições de promoção registradas neste relatório |

Terraform, Kubernetes, Helm, Dockerfiles, Compose, IAM, FortiGate e recursos cloud não foram alterados por este trabalho de UI. Não foram revalidados como se fizessem parte de uma entrega de infraestrutura.

### 8. Decisão final

- **Entrega aprovada?** Sim, com melhorias não bloqueantes, exclusivamente como implementação local revisável.
- **Pode ir para produção?** Não por esta revisão. Não houve autorização de publicação nem validação autenticada das integrações reais; o gate de produção permanece não aprovado.
- **O que precisa ser corrigido antes do merge/deploy?** Antes de merge, preservar o trabalho anterior e selecionar somente os arquivos desta entrega, repetindo os checks se o conteúdo mudar. Antes de deploy autorizado, homologar OIDC/Grafana/Alloy, validar correlação recebida e executar rollout/rollback controlado. O ajuste de marca TQI Bid continua pendente da referência.
