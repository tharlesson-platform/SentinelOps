# Estado de cobertura da documentação

Data da avaliação: 2026-08-20.

## Cobertura executável

| Tema | Estado | Referência |
|---|---|---|
| Quickstart local | Completo para laboratório | `README.md` |
| Linux single-node faseado | Completo para laboratório/piloto | `docs/installation/linux-server.md` |
| Coleta geral de host Linux | Completo para Node Exporter/logs; cAdvisor opcional | `docs/integrations/host-monitoring.md` |
| Bootstrap APM | Inicial para 11 linguagens/frameworks | `docs/integrations/apm-onboarding.md` |
| Entrega, aceite e rollback | Completo para single-node | `docs/operations/delivery-runbook.md` |
| Backup/restore/upgrade | Procedimento documentado, não provado em PRD | `docs/operations/lifecycle.md` |
| Docker Compose | Executável e validável | `deploy/compose/docker-compose.yml` |
| Kubernetes small/distributed | Parcial | chart cobre somente apps SentinelOps |
| AWS/Azure/on-premises HA | Incompleto | módulos não compõem plataforma integral |
| OIDC corporativo/mTLS | Parcial | exige homologação e PKI externa |
| Multi-tenancy/RBAC por recurso | Incompleto | bloqueador produtivo conhecido |
| Gate PromQL/LogQL/TraceQL/SLO | Incompleto | gate atual é HTTP |
| DR e restore real | Não evidenciado | executar em ambiente-alvo |
| Faro/RUM e profiling por runtime | Parcial | receiver e políticas produtivas pendentes |
| Vulnerabilidades do Alloy 1.18.1 | Bloqueador | 12 HIGH corrigíveis no scan de 2026-08-20 |

## Interpretação

A documentação agora permite instalar, entregar, operar e fazer onboarding no
perfil Linux single-node. Ela **não torna a plataforma completa nem aprovada
para produção**. Os itens parciais/incompletos são requisitos técnicos reais,
não lacunas apenas textuais.

Antes de promover para PRD, conclua os bloqueadores em
`docs/operations/production.md`, execute restore/upgrade/rollback e registre a
revisão SRE sobre evidência do ambiente-alvo.
