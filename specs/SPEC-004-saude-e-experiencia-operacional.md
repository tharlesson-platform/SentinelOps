# SPEC-004 — Saúde verdadeira e experiência operacional

**Fase:** E1/E2 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma SentinelOps

## Problema do operador

Um portal que mostra números ilustrativos, conexão presumida ou controles sem
backend mascara incidentes. O operador precisa distinguir catálogo, telemetria
consultada, dado obsoleto, indisponibilidade de API e capacidade ainda ausente.

## Escopo, contratos e UX

A SPA consulta serviços, ativos, agentes, cenários e execuções pela API
autorizada. O estado do control plane é derivado da última resposta: enquanto
carrega, mostra consulta em andamento; após falha, mostra indisponível; após
sucesso, apresenta o horário da consulta. A atualização manual repete as
requisições. A busca superior filtra o catálogo carregado e não afirma buscar
traces, releases ou dados remotos que não tenham endpoint implementado.

Métricas de disponibilidade, saúde global e degradações permanecem
`Sem dados` até existirem query, fonte, janela, timestamp e política
reproduzíveis. O catálogo exibe lifecycle, nunca o converte em saúde.

## Autorização, degradação e operação

O frontend usa o bearer token emitido pelo IdP/API e não decide autorização.
Erros preservam o request ID retornado; dados previamente carregados não
transformam uma falha atual em estado conectado. Refresh e filtros não emitem
consultas de telemetria arbitrárias.

Antes de disponibilizar drill-down de Grafana, trace, log ou release, a API
deve entregar contrato tenant-scoped com paginação, freshness, provenance e
RBAC. Caso contrário, a UI apresenta estado vazio explicando a ausência.

## Critérios

- **AC-0401:** Dada falha em qualquer consulta inicial, quando a SPA renderiza,
  então o control plane aparece indisponível, sem selo verde fixo.
- **AC-0402:** Dada resposta bem-sucedida, quando o operador atualiza, então
  o horário de consulta muda somente após a nova resposta da API.
- **AC-0403:** Dado termo digitado na busca, quando o catálogo abre, então
  serviços e ativos carregados são filtrados pelo mesmo termo.
- **AC-0404:** Dada ausência de query com janela e fonte, quando o overview
  abre, então não exibe percentual de disponibilidade, score ou degradações.

## Testes, rollout, rollback e evidência

O build TypeScript e o teste da API web são obrigatórios localmente. A jornada
E2 deve simular API indisponível e dados stale em navegador, com captura
sanitizada e request ID. Rollback reverte o bundle web; não introduz números
fixos para reduzir a percepção de degradação.
