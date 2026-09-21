# Coleta por aplicação conteinerizada — diagnóstico e execução

## Estado desta investigação

O escopo autorizado é `tqi-platform`, `easy-vm` e a plataforma SentinelOps. O objetivo é comprovar logs, métricas e traces por aplicação, incluindo visões de ERROR, HTTP 4xx e HTTP 5xx. Não há autorização para mudanças em outros hosts ou na rede/cloud.

**A investigação remota desta etapa está bloqueada por autenticação.** As sessões Termius de tqi-platform e SentinelOps foram encerradas pelo servidor/timeout. Uma tentativa não interativa em cada servidor recusou a chave. A reconexão dos três hosts e o sudo foram solicitados ao operador. Não houve alteração remota de collector, configuração, WAL ou aplicação nesta etapa.

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

Se esse caminho for adotado após o inventário: validar o arquivo, gravar temporário e usar fsync/rename no mesmo diretório; montar o **diretório** readonly no coletor, não um arquivo cujo inode será substituído. Preservar a última lista válida em falha, sinalizar idade/erro e não publicar lista vazia por falha de Docker. Não adicionar `docker.sock` ao agente de logs. Este documento não representa instalação desse mecanismo.

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

Oito testes unitários passaram: exclusão de segredos e conteúdo, limite do recorte, distinção de drivers, sanitização de endpoint, rejeição de host incorreto, isolamento de contexto Docker remoto e rejeição de daemon divergente. A validação é local com fixtures; a execução nos hosts e a cobertura da coleta continuam bloqueadas pelas sessões não autenticadas.
