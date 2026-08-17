# Plano de implementação

Data de referência: 2026-08-17. Timezone padrão: `America/Sao_Paulo`.

## Princípios

- O caminho principal é funcional e não usa mocks. O perfil `local-demo` usa
  serviços reais em contêineres e uma aplicação demonstrativa isolada.
- Nenhum `terraform apply`, alteração cloud, DNS ou deploy de produção é
  executado automaticamente.
- Imagens, módulos e pacotes usam versões exatas. Secrets locais são gerados
  em `.env`, ignorado pelo Git.
- Resultado de quality gate é determinístico; IA nunca aprova, reprova ou
  executa ações.

## Fases e critérios de saída

1. Fundação: API, Web, PostgreSQL, autenticação local, RBAC, auditoria,
   migrations e Compose com health checks.
2. Observabilidade: Prometheus, Loki monolítico, Tempo monolítico, Pyroscope,
   Grafana, Alloy e correlações provisionadas.
3. Catálogo e agentes: CRUD declarativo, bootstrap/heartbeat, localização,
   capabilities e CLI.
4. Synthetic: execução HTTP real, agendamento, evidências e Test Studio.
5. Delivery assurance: releases, políticas versionadas, checks, PASS/FAIL/
   INCONCLUSIVE, webhooks idempotentes e integração CI.
6. APM/SLO: demo instrumentada, dashboards, SLO e alertas.
7. Produção: Helm/Terraform validáveis, hardening, backup/restore e runbooks.
8. Assistente: somente leitura e desabilitado por padrão; não faz parte do gate.

Cada fase exige testes, documentação atualizada e ausência de erro crítico
conhecido antes do avanço. O relatório final diferencia claramente execução
local comprovada de exemplos de produção apenas validados estaticamente.

## Matriz de evidências

| Área | Evidência mínima |
|---|---|
| Go | `go test ./...`, `go vet ./...` |
| Web | `npm test`, `npm run build` |
| Compose | `docker compose config`, health e smoke HTTP |
| Helm | `helm lint`, `helm template` |
| Terraform | `terraform fmt -check`, `terraform validate` |
| Segurança | secret scan, redaction e replay tests |
| Gate | release saudável PASS e release degradada FAIL |

