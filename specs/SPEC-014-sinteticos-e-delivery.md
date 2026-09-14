# SPEC-014 — Sintéticos HTTP e delivery assurance

**Estado:** IN_PROGRESS · **Responsável:** Plataforma SentinelOps

## Problema do operador

Um HTTP 200, um redirect para destino não autorizado ou duas réplicas executando
o mesmo slot não comprovam disponibilidade nem segurança.

## Escopo e contratos

O runner inicialmente suporta apenas HTTP `GET`, `HEAD` e `OPTIONS`. Cada
cenário inclui URL, timeout, headers e assertions fechadas: `status_exact`,
`status_range`, `header_present`, `header_equals`, `body_contains`,
`json_path_exists`, `json_path_equals` e `latency_max_ms`. Browser/k6 ficam
rejeitados pela API até possuírem runner distribuído.

`SYNTHETIC_ALLOWED_TARGETS` usa `hostname@CIDR[|CIDR]`. O hostname e cada IP
resolvido, inclusive após redirect, precisam atender à mesma regra. Alvos
privados só entram com site e CIDR explicitamente autorizados.

## Dados, falhas e recuperação

`synthetic_execution_claims` faz claim transacional por cenário/versão/slot.
Em crash, `RUNNING` antigo é marcado `INCONCLUSIVE`, com motivo observável;
nenhuma segunda réplica duplica silenciosamente o slot. Resposta acima de 1 MiB
é falha de assertion, não evidência parcial de sucesso.

Os gates PromQL/LogQL usam `query_range`, nunca vetor instantâneo:
`*_window`, `*_step`, `*_max_age`, `*_min_samples` e `*_max` são
obrigatórios. Warnings, resposta parcial, timestamp fora da janela, evidência
obsoleta e valores `NaN`/`Inf` resultam em `INCONCLUSIVE`.

TraceQL exige tanto a query de erro quanto uma `coverage_query` com número
mínimo de traces. Uma lista vazia de erros sem ingestão confirmada não aprova
uma release.

## Critérios

- **AC-1401:** Dado um HTTP 200 com `$.ok=false`, quando há assertion JSONPath
  `$.ok=true`, então a execução falha.
- **AC-1402:** Dado redirect para host/IP não autorizado, quando o cliente o
  segue, então não abre a conexão e o run não recebe `PASS`.
- **AC-1403:** Dadas duas réplicas no mesmo slot, quando ambas tentam claim,
  então somente uma cria e executa `test_run`.
- **AC-1404:** Dado runner ausente para browser/k6, quando a API recebe esse
  tipo, então responde `unsupported_capability`.
- **AC-1405:** Dado vetor instantâneo, warning, amostra stale ou `NaN`,
  quando o gate executa, então o resultado é `INCONCLUSIVE`, não `PASS`.
- **AC-1406:** Dado TraceQL de erro vazio sem traces na query de cobertura,
  quando o gate executa, então o resultado é `INCONCLUSIVE`.

## Rollout, rollback e evidência

Primeiro configure o site de homologação como hostname+CIDR, execute E2E e
revise `connectedIPs`, slot e assertions. Para rollback, pause o cenário e
retorne à versão anterior versionada; não expanda CIDRs para fazer passar.
