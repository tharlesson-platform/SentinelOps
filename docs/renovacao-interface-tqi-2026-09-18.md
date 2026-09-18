# Renovação SentinelOps e portal TQI — entrega local

Implementação de 18/09/2026 no checkout `02-produtos/externos/sentinelops`. A etapa inicial foi concluída localmente, sem commit, push ou implantação. Posteriormente, o usuário autorizou commit e push de todas as alterações pendentes e deploy no servidor SentinelOps. A autorização não substitui a validação operacional: os resultados locais abaixo continuam distintos da evidência de produção. A gestão de usuários permanece fora do escopo. O diretório `deploy/vm/private-ingress/` e alterações em App/API/testes/Vite já existiam; foram preservados e incluídos no versionamento solicitado. Nesse diretório, a renovação visual alterou somente o HTML do portal.

## Jornadas implementadas

- Seleção explícita de servidor ou container; containers homônimos são identificados pelo par servidor + nome. Nenhuma seleção automática oculta.
- Busca em todos os servidores, ambiente quando presente no inventário, filtros independentes da seleção e ação Limpar filtros. Servidor → Containers usa `host_name` explícito para correlacionar exporters com instâncias diferentes.
- Métricas escolhem o endpoint pelo tipo de recurso. Trocar A → B cancela A e impede que dados anteriores sejam mostrados sob o novo título.
- URL em `/sentinelops/?page=...` conserva entidade, busca, filtros e janela absoluta. Início abre o dashboard; Voltar usa histórico interno e recupera filtros/seleção/rolagem, com fallback para a lista. A restauração aguarda conteúdo assíncrono até 20 s e é cancelada por outra navegação ou interação do usuário.
- Período compartilhado 15 min/1 h/6 h/24 h; Atualizar avança o término da janela e consulta a tela ativa. APM declara expressamente sua janela fixa de 5 minutos e limitações de correlação com traces.
- Séries separadas por labels e timestamp; lacunas não são unidas; legenda, unidade, última amostra por série, aviso de amostra antiga e tabela acessível. CPU do servidor usa média entre núcleos; CPU de container permanece percentual equivalente a um núcleo, podendo superar 100%, sem threshold falso de capacidade total.
- Inventário e gráficos mostram fontes e horários próprios. O SourceBanner atualiza somente o relógio de apresentação a cada 30 segundos e sinaliza consultas com mais de cinco minutos, mesmo sem interação, sem novas chamadas de telemetria. O intervalo é limpo ao desmontar ou substituir o timestamp da fonte; estados indisponível/parcial/sem dados permanecem preservados. Estados de carregamento, falha/retry, parcial, sem dados, consulta antiga e ausência de seleção são distintos. Resumo inicial usa evidências e caminhos de investigação, sem painéis de saúde inventada.
- Logs são enviados pelo formulário, cancelados ao mudar de consulta e limitados explicitamente a 300 eventos. Consulta global reúne serviços e servidores em duas buscas disjuntas, ordenadas e limitadas globalmente; falha parcial é informada. Os dois seletores são preservados no encaminhamento ao Grafana. Não há paginação que prometa completude: refine o período/texto ou continue no Explore.
- Sem `host_name` ou ID completo reconhecido do container, a busca específica fica bloqueada com explicação. A limpeza explícita consulta fontes identificadas por serviço ou servidor. A identidade Docker reconhece ID hexadecimal completo, `/docker/<id>` e `/system.slice/docker-<id>.scope`; não usa prefixos curtos como equivalência.
- Traces oferecem abertura pelo ID no Grafana. Perfis oferecem Pyroscope no Grafana com período; a seleção de aplicação/tipo de perfil ocorre na ferramenta e a limitação é visível. Sem botões de investigação fictícios.

## Identidade, idioma e acessibilidade

Logotipo reutilizado do ativo local `orgflow/apps/web/src/assets/tqi-logo.svg`, com cores de referência do mesmo projeto em `apps/web/src/tqi-theme.css`. Não foi recebido ou inspecionado o TQI Bid; não há afirmação de fidelidade a esse produto nem aprovação formal de marca. Tokens estão centralizados para o ajuste posterior.

Português é o idioma disponível nesta entrega. Foi retirado o seletor que prometia uma tradução inglesa incompleta; internacionalização integral em inglês não foi implementada. Temas claro/escuro, labels, foco visível, menu mobile expansível com Sair acessível, pular para conteúdo e diálogo nativo foram tratados. Fonte remota Google Fonts removida: a interface utiliza fallback local. Login distingue credenciais inválidas, sessão expirada e falta de permissão; callback OIDC preserva URL interna e evita processamento duplicado no StrictMode.

Portal conserva oito destinos, agrupados em Visão operacional, Investigação, Ferramentas avançadas e Administração. Mantém as proteções existentes; nenhuma regra Nginx foi alterada. O logotipo do portal é servido pelo ativo público `/sentinelops/tqi-logo.svg` no bundle.

## Contrato e compatibilidade

- Novos campos opcionais `hostName` em servidor/container e `logContainerId` em container.
- Perfis Alloy Linux/cAdvisor passam a adicionar `host_name` a partir de `SENTINEL_HOST_NAME`, sem alterar `instance`. Coletores já implantados precisam de rollout autorizado para fornecer esse label. Métricas existentes continuam consultáveis, mas correlação ausente é exibida como indisponível.
- Detalhe de container agora requer servidor: consumidores externos antigos que omitem `host` receberão erro de validação, evitando agregação ambígua de homônimos.
- APM agrupa consistentemente por serviço, ambiente e servidor. Ao investigar traces, o filtro de ambiente não é aplicado e a tela avisa que resultados podem incluir outros ambientes.
- Logs retornam `queries` e `limit` além de `query` para compatibilidade; a busca global faz no máximo duas consultas limitadas à fonte. Fontes sem `host_name` nem `service_name` não entram nessa cobertura e não são apresentadas como cobertas.
- Links seguem o [formato documentado do Grafana Explore](https://grafana.com/docs/grafana/latest/visualizations/explore/get-started-with-explore/). Trace ID é enviado ao editor TraceQL, conforme [documentação oficial](https://github.com/grafana/grafana/blob/main/docs/sources/shared/datasources/tempo-editor-traceql.md). Os UIDs usados coincidem com o provisioning local (`loki`, `tempo`, `pyroscope`). Isso não comprova login ou acesso real às ferramentas publicadas.

## Validação local

- `VITE_BASE_PATH=/sentinelops/ npm run build --prefix apps/web`: typecheck e bundle Vite aprovados; caminhos de assets e config com a base correta.
- `npm test --prefix apps/web`: 15 testes unitários aprovados.
- Go 1.26.0 temporário, obtido de go.dev e verificado por SHA-256: `go test ./internal/httpapi ./internal/telemetryquery` aprovado. Inclui correlação sem inferência, homônimos, cgroups reconhecidos, RED por identidade composta, união/ordenação/limite de logs e resposta parcial com servidor HTTP local controlado.
- Playwright: 19/19 jornadas aprovadas no bundle compilado servido com base `/sentinelops/` (5,5 s na rodada final), com fixtures controladas, cobrindo seleção, APIs corretas, A lento/B rápido, recuperação, refresh, URL/reload, navegação e scroll, quatro estados de fonte, logs sem ID, busca por formulário, portal, teclado/mobile/tema, login, permissão e APM. O teste adicional avança o relógio até o estado desatualizado, garante ausência de novas requisições, verifica a recuperação por atualização explícita e confirma a limpeza do timer ao desmontar. As 18 jornadas anteriores também foram aprovadas no servidor de desenvolvimento.
- Screenshots locais inspecionados: dashboard/portal desktop e container mobile claro/escuro. Valores nessas imagens são fixtures de teste, não telemetria da TQI.
- `git diff --check`: sem erros de whitespace.

Reprodução do navegador sem iniciar infraestrutura: inicie `VITE_BASE_PATH=/sentinelops/ npm run dev --prefix apps/web -- --host 127.0.0.1 --port 4318 --strictPort`; na raiz execute `./tests/playwright/node_modules/.bin/playwright test -c tests/playwright/playwright.local.config.ts`. Para testar o bundle, após o build inicie `VITE_BASE_PATH=/sentinelops/ ./node_modules/.bin/vite preview --host 127.0.0.1 --port 4319 --strictPort` pelo binário local de `apps/web/node_modules/.bin`, a partir de `apps/web`, na porta 4319; execute a mesma suíte com `WEB_URL=http://127.0.0.1:4319`. Relatório em `tests/playwright/artifacts/report/`; imagens em `artifacts/` (ignorados pelo Git).

## Limites e próxima etapa operacional

Esta é uma entrega local revisável. Não foram validados OIDC corporativo, Grafana/Tempo/Pyroscope autenticados, telemetria real, DNS/TLS/VPN, comandos Alloy de validação nem pipeline remoto. Docker local estava indisponível e não foi iniciado. O teste de login usa API controlada; o teste do portal verifica navegação e destinos, sem afirmar disponibilidade externa.

Antes de uma publicação separadamente autorizada: revisar este diff junto das alterações preexistentes; versionar o bundle, backend e perfis de coleta; validar Alloy e coletores em homologação; testar login/callback/permissões com o IdP real e o par métrica/log/trace de um recurso autorizado; confirmar destinos do portal pelo ingresso real. Guardar a versão anterior dos artefatos e configurações para rollback. Não restaurar ou descartar o worktree sujo para simular rollback. A mudança de labels deve ser coordenada com frontend/backend para que a correlação fique disponível.

Revisão independente: `docs/reviews/2026-09-18-renovacao-ui-sre.md`.

Ajuste complementar solicitado pela tarefa de origem: envelhecimento automático do SourceBanner resolvido e revisado independentemente. Reexecutados build/typecheck, os 15 testes unitários e as 19 jornadas Playwright; `git diff --check` limpo. Nenhuma alteração de backend ou integração foi necessária neste complemento.
