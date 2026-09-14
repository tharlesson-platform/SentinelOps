# Harness de engenharia do SentinelOps

O harness é o controle executável do desenvolvimento. Um comando que não pôde
executar deve terminar em `BLOCKED` ou `NOT_RUN`; ele nunca representa uma
verificação ausente como `PASS`.

## Comandos

| Comando | Uso |
|---|---|
| `make harness-doctor` | Inventaria ferramentas e pré-requisitos sem imprimir segredos. |
| `make harness-validate-specs` | Valida IDs, dependências, critérios e evidências declaradas. |
| `make harness-check` | Executa lint sem mutar fontes, testes unitários, build web e contratos estáticos/sintaxe dos collectors Windows e SNMP. |
| `make harness-integration` | Exige URLs isoladas de banco para o teste de RLS/migration. Aceita `SENTINELOPS_TEST_DOCKER_NETWORK` para resolver um PostgreSQL Compose sem publicar porta. |
| `make harness-e2e` | Exige imagens locais travadas e executa Playwright/k6. |
| `make harness-eval-ai` | Executa 30 cenários sanitizados do guardrail pré-provider; não chama modelo. |
| `make harness-release` | Executa os controles de manifest/release locais. |
| `make harness-production TARGET=<alvo>` | Apenas coleta evidência de alvo já autorizado; não promove. |

Cada execução deve deixar um diretório sanitizado em `artifacts/evidence/` com
SHA, diff (quando aplicável), horário, comando, resultado e links para specs.
Esse diretório é ignorado pelo Git; artefato primário de ambiente TQI não deve
ser versionado.

Quando o PostgreSQL temporário estiver publicado no host, informe as URLs com
`host.docker.internal`, pois o teste executa em container. O harness registra
esse alias como gateway do host e não aceita banco do Compose/produção.

O primeiro marco E2 permanece bloqueado até haver IdP TQI homologado, alvo
completo, restore/rollback e evidência de fontes reais. Veja
`docs/product/capability-matrix.md` e `docs/execution/checkpoint.md`.
