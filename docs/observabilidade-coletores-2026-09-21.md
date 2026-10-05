# Coleta por recurso: evidências de 2026-09-21

## Escopo

Correção da correlação de host, container e serviço no tqi-platform e
easy-vm; inclusão dos logs do driver Docker `local` do OrgFlow; ajuste das
consultas de logs do catálogo. Alterações remotas restritas aos coletores e
backends de observabilidade. O frontend Huawei NOC no tqi-platform já
apresentava reinícios antes desta intervenção.

## Evidência observada nos coletores

- Labels de host aplicadas aos perfis de métricas e logs. Identidade OTLP
  propagada sem concatenar atributos duplicados `service.name/service_name`.
- Metadata Docker sanitizada disponível por proxy Unix com whitelist. Alloy
  e Beyla não recebem o socket real do Docker nem o ambiente dos containers.
- Logs JSON preservam storage path e componentes, com nome/ID/serviço reais.
- OrgFlow: cinco containers inscritos no driver local; teste real de parada
  de 30,026 segundos recuperou no Loki os quatro registros emitidos durante
  esse intervalo. Os demais containers ficaram silenciosos nessa janela.
- A migração para `max(ativação, criação)` preservou os cinco enrollments
  byte a byte; reiniciou somente o proxy de logs. A coleta reconectou, com
  12 componentes saudáveis e fila vazia na verificação posterior.
- Easy-vm: patch Beyla restrito a cgroup Docker e nome Swarm. Sete serviços
  Java tiveram identidade correlacionada em métricas e traces após GETs de
  leitura. Respostas 401/404 não foram classificadas como aplicações saudáveis.
- Alloy central: sanitização anterior ao tail sampling e retenção de erros
  HTTP 400–599. Reinício controlado do Alloy; fila vazia e spans aceitos
  observados depois. O buffer em memória de cinco segundos pode ter perdas.
- Loki: atributos de host, ID, ambiente, origem, job e stream indexados.
  Reinício controlado de aproximadamente 19 segundos, com armazenamento,
  imagem e configuração Docker preservados.

## Contratos testados

Passaram 8 testes de proxy, 10 de inventário e 9 de dashboards. Ensaios
nativos com a imagem Alloy ativa validaram framing stdout/stderr, silêncio,
reconexão, labels, atualização de metadata, fila OTLP persistente após SIGKILL,
saturação e falha fechada em storage sem escrita. O ensaio experimental de
WAL loki.write falhou e esse modo não foi habilitado.

O catálogo contém 37 dashboards, 188 painéis e 217 targets. As consultas
separam logs Docker com ID, fallback por filename somente quando falta ID,
e fontes não Docker. Seis painéis que retornam métricas Loki usam `stat`
com `lastNotNull`. Limites são por target: até 600 registros no painel com
três consultas de 200, sem alegar total consolidado da fonte.

## Limites e operação

Silêncio significa ausência de registros na janela, não falha nem saúde.
Banco/cache e workers sem HTTP podem ter métricas/logs sem requisições APM.
O head sampling de 25% dos SDKs Java existentes limita os traces recebidos;
tail sampling não recupera spans descartados no SDK. Persistem limitações
anteriores de instrumentação Java/Node no easy-vm.

A fila local tem fsync e capacidade lógica de 64 MiB, sem promessa de
durabilidade ponta a ponta: há janela entre cursor e enqueue, erros
permanentes, falhas de disco e ACK do batch central anterior ao Loki.
Retenção Docker limita recuperação e o corte usa segundos. Nunca apagar ou
retroceder WAL, posições e enrollments para forçar leitura.

Procedimentos e rollback: [docker-read](../deploy/agents/linux/docker-read/README.md)
e [patch Beyla](../deploy/beyla/README.md). Evidências detalhadas ficam nos
artifacts locais e em `/var/tmp/sentinelops-phase2-20260921` dos hosts,
sem incluir corpos de logs ou credenciais no Git.

Este registro comprova os coletores e contratos descritos. A publicação do
catálogo embutido na API, do Grafana e a aceitação autenticada dos seletores
devem ser registradas separadamente com o SHA efetivamente publicado.
