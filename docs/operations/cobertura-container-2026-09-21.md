# Coleta por aplicação conteinerizada — diagnóstico e execução

## Estado desta investigação

O escopo autorizado é `tqi-platform`, `easy-vm` e a plataforma SentinelOps. O objetivo é comprovar logs, métricas e traces por aplicação, incluindo visões de ERROR, HTTP 4xx e HTTP 5xx. Não há autorização para mudanças em outros hosts ou na rede/cloud.

**A coleta remota foi retomada após a autenticação do operador.** Os coletores foram corrigidos nos dois hosts. A matriz abaixo registra a evidência efetivamente observada; a aceitação da nova API/web e dos dashboards permanece separada até comprovar o SHA publicado. Os achados estáticos seguintes preservam o diagnóstico inicial e não substituem o estado atualizado.

As contagens da validação anterior de interface não são o denominador desta investigação: resultados limitados a 300 eventos/100 requisições não comprovam cobertura de todos os containers. O inventário precisa ser recolhido ao vivo e reconciliado no mesmo intervalo das fontes.

## Inventário preparado

`scripts/inspect-container-observability.py` executa somente operações de inspeção Docker/host e grava um artefato privado `0600`. Exige correspondência do hostname antes de chamar Docker. Fixa os comandos no socket Unix local e remove variáveis que selecionam contextos remotos; também confere `docker info.Name` antes de listar containers. Um socket local alternativo exige `--docker-socket` explícito. Execute no host correto, após transportar e conferir o hash do script:

```sh
sudo python3 inspect-container-observability.py \
  --expected-host tqi-platform \
  --output /var/tmp/sentinelops-observability/tqi-platform-inventory-UTC.json
```

Use o hostname correto em cada sessão e substitua `UTC` por um timestamp único. O programa não instala agentes, não reinicia containers e não altera logs. O arquivo contém IDs, imagens, drivers, rotação, portas, mounts, saúde, reinícios, nomes dos processos, indícios de instrumentação, destinos sem credenciais/query, hashes das configurações de coletores e uso de recursos. Não exporta mensagens de logs, ambientes completos, comandos, labels arbitrários, saídas de health checks nem valores de segredos.

A leitura de cada arquivo `json-file` é limitada aos últimos 256 KiB. Exporta somente metadados, timestamps, streams e contadores. Busca textual de ERROR não é classificação exata de severidade. Contagem de status considera somente campos estruturados; números soltos não são classificados como HTTP. Campos genéricos como `status`/`statusCode` ainda dependem da confirmação do esquema da aplicação; os contadores são indícios, não prova isolada de resposta HTTP. Timestamps são normalizados para UTC com precisão de microssegundos e servem à recência, não à comparação exata de eventos com nanossegundos. Drivers diferentes de `json-file` ficam explicitamente sem leitura de conteúdo pelo script. Ausência de mensagens nesse recorte não prova silêncio no período completo.

Além do artefato, verificar versões, arquivos ativos e configuração de cada agente, volume/offsets/filas/WAL, permissões dos diretórios, certificados e SNI, destinos e tenant, filas de envio, rejeições e recursos do host. Capturar apenas metadados e erros sanitizados. Não copiar chaves ou ambientes completos para evidências.

## Achados estáticos que precisam ser reconciliados com o ambiente

| Configuração no repositório | Efeito possível | Verificação/ação necessária |
|---|---|---|
| Logs Docker usam `*-json.log` | Drivers `local`, journald ou logs somente em arquivos não são lidos por esse pipeline | Levantar driver/LogPath e destino real de cada aplicação; preparar leitor apropriado sem trocar o driver de negócio indiscriminadamente |
| Logs não recebem `service_name` e `container_name` | Eventos chegam por host/ID, mas aplicação não aparece pelo nome esperado | Mapear a identidade por container e validar o mesmo nome emitido por SDK/Beyla; não inferir equivalência automaticamente |
| Métricas exigem `host_name` compartilhado | Configuração antiga pode impedir a correlação entre exporters e logs | Comparar hash ativo com repo e consultar labels atuais; `instance` não substitui automaticamente `host.name` |
| Beyla seleciona apenas portas 80, 3000, 3001, 8080 e 8083 | Processos em outras portas podem ficar fora da descoberta | Inventariar portas/runtimes e selecionar processos HTTP/gRPC por identidade aprovada; trace de proxy não comprova instrumentação interna |
| Overlays SDK cobrem duas aplicações Java | Outras aplicações podem ter apenas observação de proxy ou não ter traces | Conferir runtime, agente real, versão/hash, contexto e dependências; instrumentar gradualmente cada aplicação elegível |
| SDK Java usa head sampling de 25%; central faz tail sampling | Eventos descartados na origem não podem ser recuperados no coletor | Escolher orçamento e política após medir volume/capacidade; não prometer retenção de todos os erros com head sampling parcial |
| Tail sampling considera status ERROR e amostra geral | HTTP 4xx de spans servidor pode permanecer UNSET | Criar políticas/visões separadas de 4xx, 5xx e falhas de span, considerando atributos atuais e legados sem alterar convenções |
| Redaction de atributos está ligada a logs OTLP, não traces | Atributos sensíveis instrumentados podem passar nos traces | Validar sanitização de todos os sinais antes do envio; não habilitar captura de bodies/headers sensíveis |
| Regra `stage.drop` descarta a linha inteira por palavras como `token=` | Logs úteis podem ser perdidos junto do campo sensível | Validar redaction por parser/formato e contadores; nunca remover proteção sem substituição testada |
| cAdvisor usa WAL em tmpfs; Alloy principal e logs compartilham volume | Recreate pode perder backlog ou encontrar diferenças de ownership | Levantar conteúdo/UIDs antes de migrar; preservar checkpoints/volumes e evitar replay inadvertido |
| Endpoint SDK usa bridge Docker, bind padrão do receiver é loopback alternativo | Endpoint pode ser inalcançável do container | Verificar bind/bridge efetivos com chamada segura; não publicar OTLP indiscriminadamente |
| Métricas RED podem ser geradas por Beyla e Tempo | Fontes antes/depois da amostragem representam populações distintas | Identificar famílias/labels e evitar somar contagens duplicadas ou usar RPS amostrado como tráfego total |

A hipótese de que `stage.regex source="filename"` nunca lê esse label foi descartada para o upstream Alloy v1.18.1: o pipeline copia os labels iniciais para o mapa extraído. Ainda é necessário confirmar binário, configuração ativa e streams recentes no ambiente.

## Enriquecimento de logs sem socket no coletor

Uma opção compatível com Alloy v1.18.1 é um processo de inventário no host gerar um array JSON de targets, consumido por `local.file` → `encoding.from_json` → `local.file_match`. Os targets precisam usar LogPath verificado, ID completo e nomes efetivos. A escolha de `service_name` depende da matriz de instrumentação.

Se esse caminho for adotado após o inventário: validar o arquivo, gravar temporário e usar fsync/rename no mesmo diretório; montar o **diretório** readonly no coletor, não um arquivo cujo inode será substituído. Preservar a última lista válida em falha, sinalizar idade/erro e não publicar lista vazia por falha de Docker. Não adicionar `docker.sock` ao agente de logs. O mecanismo complementar instalado e seus limites estão descritos no [runbook docker-read](../../deploy/agents/linux/docker-read/README.md).

## Matriz e aceite obrigatório por aplicação

Registrar cada container do inventário, inclusive parado e infraestrutura, com: host/ID/nome/imagem/runtime, serviço lógico verificado, driver/destino, logs emitidos no intervalo, logs observados no Loki, métricas presentes, instrumentação/nível de trace, status HTTP, correlação trace/span IDs, erros da coleta e prova de apresentação em Grafana/SentinelOps.

Os estados são independentes: **confirmado**, **sem emissão observada**, **não instrumentado**, **falha de coleta**, **incompatível** ou **bloqueado**. Um banco sem HTTP ou um job sem execução não recebe falsa saúde APM. Contagem Docker é o denominador; listar explicitamente exclusões e motivos.

Reconciliar streams por host e ID completo usando agregações/series e janelas registradas, não uma página de eventos. Para traces, procurar atributos de recurso por aplicação e execução real; não usar apenas `rootServiceName` de uma amostra como inventário completo. Separar volume de requisições, spans, traces e métricas calculadas após amostragem.

Rollout: baseline/configuração e volumes preservados → validação local com a imagem exata → um host/agente → janela de observação e filas/capacidade → segundo host → aplicações instrumentadas individualmente. Reinícios de negócio exigem análise de impacto/health/rollback específica. Não reiniciar a unit de instrumentação como ajuste de collector: seu `ExecStop` pode parar aplicações.

Não gerar falhas destrutivas para fabricar 5xx. Usar tráfego real ou uma ação segura aprovada, identificada como validação e sem conteúdo sensível. Aceite final exige eventos/traces reais por aplicação e filtros corretos nas duas interfaces. Todo item não verificado continua pendente.

## Referências primárias

- [Semântica HTTP do OpenTelemetry](https://opentelemetry.io/docs/specs/semconv/http/http-spans/): status HTTP e estado do span são conceitos distintos.
- [Descoberta de serviços do Beyla](https://grafana.com/docs/beyla/latest/configure/service-discovery/): critérios de descoberta e seleção de processos.
- [Processamento de logs no Alloy](https://grafana.com/docs/alloy/latest/reference/components/loki/loki.process/): parsing, labels, redaction e descarte.
- [Pipeline Alloy v1.18.1](https://github.com/grafana/alloy/blob/v1.18.1/internal/component/loki/process/stages/pipeline.go#L113-L120): inicialização do mapa extraído com os labels.

## Testes da ferramenta de diagnóstico

Dez testes unitários passaram, incluindo compatibilidade de timestamps e inventário de processos: exclusão de segredos e conteúdo, limite do recorte, distinção de drivers, sanitização de endpoint, rejeição de host incorreto, isolamento de contexto Docker remoto e rejeição de daemon divergente. Essa validação usa fixtures; as evidências remotas e os ensaios nativos são apresentados a seguir.


## Matriz observada após as correções

Inventário dos hosts às 21:50 UTC; consultas independentes Prometheus/Loki/Tempo na janela **21:25:30 UTC a 22:25:30 UTC**, sem erros de fonte. Total: 70 containers existentes, dos quais 46 com estado `running`, um em `restarting` e 23 encerrados. Entre os 47 ativos/reiniciando há 38 de negócio e nove de observabilidade. Os 23 encerrados são 19 de negócio e quatro coletores antigos; não são denominador de disponibilidade de aplicações em execução.

Todos os 47 ativos/reiniciando têm métricas de container. Loki contém eventos de 28; os outros 19 foram conferidos com `docker logs` na **mesma janela**, todos com zero registros, comando concluído e sem truncamento. Isso comprova silêncio nesse intervalo, não saúde. A busca por host e ID (64 ou prefixo único de 12 caracteres) encontrou traces de 22 aplicações; limite de um resultado comprova presença, não quantidade total.

Na tabela, “emitidos” significa eventos presentes no Loki; “silencioso” significa ausência também confirmada no Docker; “ERROR” é contagem por regex error/fatal/panic/exception, não classificação estruturada universal. “Sem trace” é ausência no intervalo, sem afirmar falha. As duas aplicações com SDK Java estão identificadas; o restante dos traces corresponde à observação eBPF de protocolo.

### tqi-platform

| Container | Papel / estado | Métricas | Logs / ERROR | Trace |
|---|---|---|---|---|
| `sentinelops-linux-collector-local-container-logs-1` | coletor / running | Presentes | 221 emitidos; 38 ERROR | Sem trace |
| `sentinelops-linux-collector-container-logs-1` | coletor / running | Presentes | 9 emitidos; 0 ERROR | Sem trace |
| `sentinelops-linux-collector-beyla-1` | coletor / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-platform-worklog-backend-1` | negócio / running | Presentes | Silencioso na fonte | Presente; SDK Java |
| `tqi-platform-scale-management-backend-1` | negócio / running | Presentes | 624 emitidos; 600 ERROR | Presente; SDK Java |
| `sentinelops-linux-collector-alloy-1` | coletor / running | Presentes | 5 emitidos; 0 ERROR | Sem trace |
| `sentinelops-linux-collector-container-metrics-1` | coletor / running | Presentes | 5 emitidos; 0 ERROR | Sem trace |
| `sentinelops-linux-collector-collector-certs-init-1` | coletor / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `sentinelops-linux-collector-alloy-init-1` | coletor / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `tqi-platform-huawei-noc-frontend-1` | negócio / restarting | Presentes | 594 emitidos; 0 ERROR | Sem trace |
| `tqi-platform-helpdesk-proxy-1` | negócio / running | Presentes | 1441 emitidos; 0 ERROR | Presente; eBPF |
| `tqi-platform-traefik-1` | negócio / running | Presentes | 3 emitidos; 0 ERROR | Presente; eBPF |
| `tqi-platform-gp-mailserver-1` | negócio / running | Presentes | Silencioso na fonte | Presente; eBPF |
| `tqi-platform-service_sso-1` | negócio / running | Presentes | 51120 emitidos; 1420 ERROR | Presente; eBPF |
| `tqi-platform-worklog-frontend-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-platform-scale-management-frontend-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-platform-teams-manager-frontend-1` | negócio / running | Presentes | Silencioso na fonte | Presente; eBPF |
| `tqi-platform-teams-manager-backend-1` | negócio / running | Presentes | 22251 emitidos; 10933 ERROR | Presente; eBPF |
| `tqi-platform-news-executive-frontend-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-platform-news-executive-backend-1` | negócio / running | Presentes | Silencioso na fonte | Presente; eBPF |
| `tqi-platform-ia-dashboard-frontend-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-platform-ia-dashboard-backend-1` | negócio / running | Presentes | Silencioso na fonte | Presente; eBPF |
| `orgflow-web-1` | negócio / running | Presentes | 179 emitidos; 0 ERROR | Presente; eBPF |
| `orgflow-ldap-writeback-1` | negócio / running | Presentes | 368 emitidos; 0 ERROR | Presente; eBPF |
| `orgflow-api-1` | negócio / running | Presentes | 354 emitidos; 0 ERROR | Presente; eBPF |
| `orgflow-postgres-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `orgflow-redis-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-platform-huawei-noc-postgres-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-platform-huawei-noc-redis-1` | negócio / running | Presentes | Silencioso na fonte | Sem trace |

### easy-vm

| Container | Papel / estado | Métricas | Logs / ERROR | Trace |
|---|---|---|---|---|
| `sentinelops-linux-collector-beyla-1` | coletor / running | Presentes | 54 emitidos; 24 ERROR | Sem trace |
| `sentinelops-linux-collector-container-logs-1` | coletor / running | Presentes | 2 emitidos; 0 ERROR | Sem trace |
| `sentinelops-linux-collector-container-metrics-1` | coletor / running | Presentes | 1 emitidos; 0 ERROR | Sem trace |
| `sentinelops-linux-collector-alloy-1` | coletor / running | Presentes | 3 emitidos; 0 ERROR | Sem trace |
| `tqi-data_redis.1.ynp4f7wvjxkrt9wr5n8dzoeer` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `tqi-data_postgres.1.nl8fbepzcf2hmeg13qhaeszkj` | negócio / running | Presentes | 24 emitidos; 0 ERROR | Sem trace |
| `schedules.1.c25kcfdo93hn223s1es316atz` | negócio / running | Presentes | 148 emitidos; 6 ERROR | Presente; eBPF |
| `collaborator.1.ynqkyf4a1z6w7e8mddk8x5m6o` | negócio / running | Presentes | 24 emitidos; 0 ERROR | Presente; eBPF |
| `easyadmin.1.fxayosvt1yckwydk6u1qxzad1` | negócio / running | Presentes | 7 emitidos; 0 ERROR | Sem trace |
| `hotsite.1.x1mtsjjp3687ju2tqrtgld0ee` | negócio / running | Presentes | 5 emitidos; 0 ERROR | Sem trace |
| `traefik.1.qdxmrlmhrnezesjqhqsmkqz7l` | negócio / running | Presentes | 5373 emitidos; 12 ERROR | Presente; eBPF |
| `repositories_mysql.1.x7tvcqghkmsgtoqi6g773ajgs` | negócio / running | Presentes | 2 emitidos; 2 ERROR | Sem trace |
| `tqieasy.1.jpd0ixsaafxwstmg9tqxdd6r1` | negócio / running | Presentes | 98 emitidos; 2 ERROR | Presente; eBPF |
| `filebrowser.1.nb02axbi8ci7pn8isv9jbv7eb` | negócio / running | Presentes | Silencioso na fonte | Presente; eBPF |
| `patrimony.1.ftdq15c06wzlgh2ws9f3m02wk` | negócio / running | Presentes | 2 emitidos; 0 ERROR | Presente; eBPF |
| `merit-money.1.6w0onp28orh8eggzpupckv1zk` | negócio / running | Presentes | Silencioso na fonte | Presente; eBPF |
| `wordpress.1.g1rfquhpxnf9z7uqzknzamrkw` | negócio / running | Presentes | 315 emitidos; 212 ERROR | Presente; eBPF |
| `gp-nginx.1.w3h1ruzf0qw3kivrskg450osq` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `candidate.1.1tu2cm2y818ob7f9m6jj0qn9u` | negócio / running | Presentes | 135936 emitidos; 21396 ERROR | Presente; eBPF |
| `rabbitmq.1.dglgfoyqmvwzt0ajj38r9ajkj` | negócio / running | Presentes | Silencioso na fonte | Sem trace |
| `sentinelops-linux-collector-alloy-init-1` | coletor / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `sentinelops-linux-collector-collector-certs-init-1` | coletor / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `gp-mailserver.1.vmx9wtxl7zl3dqlc1ffrp5b00` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `traefik.1.n2v5nilgfyypi34wxa8zf4aym` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `tqi-data_redis.1.ldd33eelg4128tf8cygdvzyht` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `tqi-data_postgres.1.hzs7lb5805y5gxxsjjc71hepd` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `gp-mailserver-rabbitmq.1.6qlbox98z3a6m6iyzhgcqkyfd` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `repositories_mysql.1.wdwys29tw038crokq6sy69ys8` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `service_sso.1.dhqj8jggju87bgrzuhpyi0nz3` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `collaborator.1.l4fmhu31rxwddpi8wibrxzj38` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `rabbitmq.1.wg0ssrn05lafzm7d34eg9ploa` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `tqieasy.1.zixrptjffm9tw14k2b3eljfdx` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `filebrowser.1.2pwda6qo2r0i9y4azgh2ppane` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `patrimony.1.8fh5x6a0mdwamj856e7ylowsx` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `merit-money.1.ouj0vj26he8f8ttbt7ghczu9a` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `schedules.1.ruttn47p01u34k0vw4fhtxt09` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `wordpress.1.f7vfbvo1ul6dk5n6t205hpoxa` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `easyadmin.1.loi2uibfpalpa2viwhnxr8pmu` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `candidate.1.p1br7pdopf8up0sckyfud0oh1` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `gp-nginx.1.9vjwd592ei1skhp7nf3vfl0rs` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |
| `hotsite.1.4c6cu0rjyv1fs5qntiq5xb5sr` | negócio / exited | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa | Encerrado; fora da janela ativa |

### Validação HTTP complementar

Às 22:29:48–49 UTC, sete frontends sem tráfego observado receberam um GET `/` de leitura no namespace de rede do próprio container: worklog, scale-management, news-executive e ia-dashboard (tqi-platform), easyadmin e hotsite responderam 200; gp-nginx respondeu 301. Não foram seguidos redirects nem preservados corpos. IDs/StartedAt permaneceram iguais. Trata-se de prova de protocolo interno, não da jornada externa de login.

Depois dessas chamadas, foram observadas 29 identidades de serviço nos histogramas HTTP, todas com host, serviço e container. Os oito containers de negócio em execução restantes são PostgreSQL/Redis/MySQL/RabbitMQ, sem servidor HTTP de aplicação capturado por este perfil. O Huawei NOC frontend continua reiniciando, condição preexistente. Ausência de requisições HTTP desses recursos não impede métricas e logs; APM de banco/mensageria exige instrumentação apropriada.

A coleta separa HTTP 4xx/5xx de status ERROR do span. O alerta ApplicationErrorRateHigh cobre somente 5xx acima de 5% por dez minutos; p95 acima de um segundo também tem regra por host/serviço/container. Logs ERROR têm visão de investigação, sem regra de alerta Loki específica nesta entrega. O teste de regras inclui os dois hosts, SDK/Beyla, peer saudável com mesmo serviço e host, 4xx isolado e host externo ao piloto.

### Evidências e limites

Arquivos sanitizados em `artifacts/collectors-2026-09-21/`, mantidos fora do Git:

- `coverage-live.json` — SHA-256 `7b065770e746485ffbbbed8a925dc91d1c58d0e4a3f04e9dbe1f4d219ee6dc54`.
- `tqi-platform-silent-check.json` — SHA-256 `0130f7c22f828c1ed8379339ecac1dcc4a6811193a443000a249a5215fe083fe`.
- `easy-vm-silent-check.json` — SHA-256 `b5de56953d9d0b0aa0a515c4bc6378ef12f40bec5b44a1314b3c9cd907d50856`.
- `tqi-platform-frontend-probe.json` — SHA-256 `11d268003c6171878c69c558027411033db85aa6ea3955e915ea69f59f26efcc`.
- `easy-vm-frontend-probe.json` — SHA-256 `4b86ba52410b344e853d6c745f7b40099275c439050a2255603bc134a63b0830`.
- `live-http-bucket-identity.json` — SHA-256 `091d31de65e143bea08315ad2966a9390b42ea9967fc6235f669661aed0f503f`.
- `business-preservation.json` — SHA-256 `b550bc6139a3cc7ffb3a9d77351d761b1965d80d24ee727979776034b1a35455`.

O inventário completo preserva os encerrados e os erros de inspeção. Houve 56 containers de negócio com ID/StartedAt preservados; essa contagem inclui os 19 encerrados. O único StartedAt de negócio alterado foi o Huawei NOC frontend que já apresentava crashloop. Sampling e durabilidade estão detalhados no [registro dos coletores](../observabilidade-coletores-2026-09-21.md).

### Confirmação dos sete frontends no Tempo

Após consultas limitadas adicionais à mesma rota GET `/`, foi confirmado pelo menos um trace de cada um dos sete frontends, selecionado por host e ID real do container. As chamadas foram encerradas ao obter evidência e não alteraram o sampling global. A matriz anterior continua representando sua janela original; o complemento comprova 29 serviços HTTP com métricas e ao menos um trace observado entre as duas verificações (dois SDK Java e 27 eBPF).

| Host | Serviço observado | GETs de validação por container |
|---|---|---:|
| tqi-platform | worklog-frontend | 16 |
| tqi-platform | scale-management-frontend | 16 |
| tqi-platform | news-executive-frontend | 16 |
| tqi-platform | ia-dashboard-frontend | 1 |
| easy-vm | easyadmin | 16 |
| easy-vm | hotsite | 21 |
| easy-vm | gp-nginx | 16 |

Evidência: `frontend-trace-proof.json`, SHA-256 `4e907ef5de27b6659ccc8db4b791836549a4be4b2e40ad04725aa6fb3a628835`. As respostas observadas permaneceram 200/301 e o StartedAt dos containers não mudou. Nenhuma falha 5xx foi fabricada para o teste.
