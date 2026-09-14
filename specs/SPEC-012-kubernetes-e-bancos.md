# SPEC-012 — Kubernetes, orquestração e bancos

**Fase:** E3 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma/SRE

## Problema, escopo e contratos

Kubernetes conecta nodes, pods, workloads, eventos, readiness, restarts, OOM,
quotas, volumes, ingress e DNS a serviço/asset estável. Packs de PostgreSQL,
MySQL, SQL Server, Redis e filas usam métricas e logs sanitizados, sem SQL
sensível por padrão.

## Dados, UX, autorização e operação

ServiceAccount é read-only, não lê secrets nem modifica workloads. Labels de
pod/controlador obedecem orçamento de cardinalidade. Coletores privados não
interferem no tráfego de aplicação.

## Critérios e evidência

- **AC-1201:** Dada falha de workload de homologação, quando ocorre, então
  relaciona pod, node, evento, log, trace e serviço.
- **AC-1202:** Dada conta de coleta, quando tenta ler secret ou modificar
  workload, então RBAC nega a ação.
- **AC-1203:** Dada explosão de pods, quando coleta, então cardinalidade e uso
  de recursos ficam dentro do orçamento definido.
- **AC-1204:** Dado banco com saturação/lock, quando a condição surge, então
  métrica e alerta têm contexto sem capturar query sensível.

Teste em cluster isolado e banco de homologação. Rollout por namespace; rollback
remove ServiceAccount/binding e DaemonSet sem alterar workloads.

## Corte implementado e lacunas

`apps/kubeinventory` coleta nodes e objetos de namespaces explicitamente
declarados por `kubectl`, usa UID como identidade estável e escreve snapshot
atômico para o agente existente. Antes de coletar, ele prova que a identidade
lista apenas os recursos necessários e nega `get secrets` e `create
deployments`. O RBAC versionado separa a permissão global mínima dos
RoleBindings renderizados por namespace.

Testes locais cobrem a descoberta limitada e a negação de segredo/escrita.
Ainda faltam cluster isolado, eventos, métricas kubelet/kube-state-metrics,
logs/traces, cardinalidade sob escala, coleta de bancos e os quatro critérios
de aceite em alvo.

O corte PostgreSQL adiciona um exporter de estatísticas agregadas read-only de
conexões, waits, locks e deadlocks. Ele exige DSN TLS validado em arquivo 0600,
não contém SQL de escrita nem query text e fica not-ready após coleta vencida.
Ainda faltam banco de homologação, alerta, comparação com estatística nativa e
packs MySQL, SQL Server, Redis e filas.
