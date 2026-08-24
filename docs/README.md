# Portal de documentação

Este índice é o ponto de entrada para operar o SentinelOps sem conhecimento
prévio do projeto. Os guias distinguem laboratório, piloto single-node e
produção; não promova um perfil porque ele apenas iniciou containers.

## Quero começar agora

| Perfil | Leia primeiro | Depois valide com |
|---|---|---|
| Pessoa avaliadora | [Primeiros 30 minutos](user-guide/first-30-minutes.md) | `make prove-local` |
| Administrador Linux | [Bootstrap do zero](installation/bootstrap-zero-to-running.md) | `make doctor` |
| Time de infraestrutura | [Monitorar hosts](integrations/host-monitoring.md) | prova mTLS do collector |
| Time de aplicação | [Onboarding APM](integrations/apm-onboarding.md) | prova de métricas, logs e traces |
| SRE/Plataforma | [Arquitetura](architecture/overview.md) e [fluxos](architecture/system-flows.md) | `make check` e `make validate-release` |
| Operação de produção | [Produção](operations/production.md) | gates externos do alvo real |

## Mapa completo

- **Arquitetura:** [visão geral](architecture/overview.md),
  [fluxos](architecture/system-flows.md),
  [versões](architecture/component-versions.md) e [ADRs](architecture/decisions/).
- **Instalação:** [Linux faseado](installation/linux-server.md) e
  [bootstrap zero-to-running](installation/bootstrap-zero-to-running.md).
- **Integrações:** [hosts Linux](integrations/host-monitoring.md) e
  [APM de aplicações](integrations/apm-onboarding.md).
- **Operação:** [entrega](operations/delivery-runbook.md),
  [produção](operations/production.md), [release](operations/release-process.md),
  [lifecycle](operations/lifecycle.md) e [isolamento](operations/database-tenancy.md).
- **Segurança:** [threat model](security/threat-model.md) e
  [política de reporte](../SECURITY.md).
- **Contratos:** [OpenAPI](api/openapi.yaml).
- **Runbooks:** [resposta operacional](runbooks/operational-response.md).
- **Evidências:** [status atual](operations/documentation-status.md) e
  arquivos datados em [validation](validation/).

## Regra de evidência

Um componente `running`, uma página HTTP 200 ou um template Helm válido não
provam produção. A promoção exige o SHA exato com CI terminal verde, artefatos
por digest e assinatura, políticas no cluster, restore e rollback ensaiados,
IdP homologado e observabilidade viva no ambiente alvo.
