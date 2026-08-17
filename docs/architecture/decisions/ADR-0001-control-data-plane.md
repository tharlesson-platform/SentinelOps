# ADR-0001 — Separar Control Plane e Data Plane

- Status: aceita
- Data: 2026-08-17

## Decisão

O Control Plane próprio mantém catálogo, identidade, agendamento, políticas e
evidências. O Data Plane usa projetos CNCF/Grafana para telemetria. Essa
separação reduz acoplamento, permite agentes em redes privadas e evita criar
outro backend de métricas/logs/traces.

## Consequências

Grafana continua sendo o mecanismo gráfico; o produto oferece contexto,
workflows e UX própria. Falha de um backend de telemetria produz resultado
`INCONCLUSIVE`, nunca sucesso implícito.

