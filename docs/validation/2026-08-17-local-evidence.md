# Evidência de validação local — 2026-08-17

Escopo: laboratório Docker Desktop em macOS, arquitetura arm64. Esta evidência
não representa aprovação de produção.

## Resultado funcional

- `docker compose up -d --build`: stack inicializada com PostgreSQL, Temporal,
  MinIO, API, worker, agente, web, Prometheus, Loki, Tempo, Pyroscope, Alloy,
  Grafana, Keycloak e Mailpit.
- `scripts/doctor.sh`: todos os endpoints de readiness responderam.
- Seed: um serviço e um cenário criados.
- Gate saudável: `COMPLETED/PASS`, resumo `health endpoint respondeu dentro do
  threshold`.
- Gate com `fault=error`: `COMPLETED/FAIL`, resumo `status HTTP 500 fora do
  intervalo 2xx-3xx`.
- Playwright: 1 teste E2E aprovado, incluindo login e ausência de erro de
  console.
- k6: 150 iterações, 300 checks, 0 falhas e p95 de 1,94 ms no último run.
- Reinício do agente: registro idempotente, credencial rotacionada, heartbeat
  aceito e quatro capabilities persistidas.

## Telemetria consultada

- Prometheus retornou três séries `demo_http_requests_total`.
- Loki retornou streams OTLP com `service_name=sentinel-demo-api`.
- Tempo retornou cinco traces para `service.name=sentinel-demo-api`.
- Métricas internas do Pyroscope registraram o dataset
  `sentinel-demo-api`, comprovando ingestão dos perfis pprof pelo Alloy.
- Grafana provisionou quatro datasources correlacionados e 35 dashboards
  gerenciados.

## Validação de código, IaC e segurança

- Go: `go test -race ./...` e `go vet ./...` aprovados.
- Cobertura Go global medida: **1,7%**. Portanto, o requisito de 80% nos
  componentes críticos não foi atingido e permanece bloqueador de produção.
- Frontend: Vitest, build TypeScript/Vite e `npm audit --omit=dev` aprovados;
  zero vulnerabilidades reportadas.
- Helm: lint e render dos perfis small/distributed aprovados.
- Terraform: fmt e validate aprovados para os dois módulos e o ambiente dev.
- GitHub Actions: `actionlint` aprovado.
- `govulncheck`: nenhuma vulnerabilidade alcançável no código; uma
  vulnerabilidade em módulo requerido sem caminho de chamada.
- Trivy filesystem: zero HIGH/CRITICAL, zero secrets e zero misconfigurations
  nos alvos reconhecidos.
- Trivy nas cinco imagens próprias: zero HIGH/CRITICAL após atualização
  explícita dos pacotes corrigidos do Alpine.

## Limites da evidência

Não foram executados cluster Kubernetes real, cloud, IdP federado, restore de
backup, caos distribuído, DR, upgrade/rollback nem pipelines remotos. O motor de
gate executável cobre HTTP; PromQL/LogQL/TraceQL, browser/k6 distribuído e os
demais módulos avançados permanecem backlog conforme o README.
