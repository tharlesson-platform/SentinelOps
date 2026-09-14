# SPEC-015 — Alertas e incidentes

**Estado:** IN_PROGRESS · **Responsável:** Plataforma SentinelOps

## Problema e escopo

Um alerta não encerra a capacidade se não possui owner, entrega, ACK,
escalonamento e resolução auditáveis. Esta fatia cria incidentes deduplicados,
timeline, ACK, resolução, auditoria no mesmo commit e fila de entrega.

## Contratos e segurança

Incidentes usam severidade P1–P4 e estados `OPEN`, `ACKNOWLEDGED` e
`RESOLVED`. `deduplicationKey` mantém um incidente aberto por evento
correlacionado. Apenas papel com `incident:write` pode mutar o ciclo.

## Critérios

- **AC-1501:** criação repetida com a mesma chave retorna o incidente aberto.
- **AC-1502:** ACK e resolução geram timeline, auditoria e outbox atomicamente.
- **AC-1503:** fila pendente não é apresentada como entrega externa confirmada.
- **AC-1504:** Dado incidente OPEN sem ACK após o intervalo da rota, quando o
  worker processa a agenda durável, então cria uma entrega ao target de
  escalonamento exatamente uma vez.
- **AC-1505:** Dado incidente ACKNOWLEDGED ou RESOLVED antes do prazo, quando o
  worker processa a agenda, então cancela o escalonamento e não envia o canal
  secundário.

## Dados, rollout e evidência

Uma rota webhook pode declarar opcionalmente
`escalation: { after: "15m", targetRef: "plantao-secundario" }`. O intervalo
aceito é de 1 minuto a 7 dias. `incident_escalations` mantém agendamento,
cancelamento e disparo sob RLS; a entrega externa continua em
`notification_deliveries` e só se torna `DELIVERED` após 2xx.

Homologue primeiro um canal secundário autorizado e uma janela curta. Para
rollback, desabilite a rota; não apague filas ou marque entregas pendentes como
entregues.
