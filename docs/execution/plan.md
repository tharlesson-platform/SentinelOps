# Plano executável por fatias

## Marco E2 — produção interna

| Fatia | Dono | Dependências | Critério para avançar | Risco atual |
|---|---|---|---|---|
| Confiança, UI e harness | Plataforma | nenhuma | checks locais e evidência compatível | E2E de navegador ainda depende do stack travado |
| Identidade e RBAC | Segurança/IdP TQI | SPEC-002 | login, MFA, remoção de acesso e RBAC homologados | IdP corporativo não conectado |
| Inventário e frota | Plataforma/Infra | SPEC-003, SPEC-005 | fonte read-only, mTLS e reconciliação em alvo | CA/gateway e hosts autorizados pendentes |
| Linux/Docker e APM | SRE/aplicações | SPEC-006, SPEC-013 | sinais e falha controlada em fonte real | escopo de hosts e gateways não confirmado |
| Incidentes e entrega | Operações | SPEC-015 | canal de teste, ACK e escalonamento reais | canal externo não homologado |
| Release/DR | Plataforma | SPEC-018, SPEC-020 | render, restore, rollback e SHA/digest promovido | alvo, capacidade e owners pendentes |

## Caminho integral E3/E4

Depois do gate E2, executar as famílias Windows/AD, rede, VMware, clouds,
Kubernetes e bancos em ondas por site e owner (SPEC-007 a SPEC-012). IA/MCP,
RUM/capacidade e expansão só avançam após isolamento de consulta, políticas de
dados e avaliações versionadas (SPEC-016 a SPEC-020).

## Regras de parada

Não há deploy automático. Qualquer ausência de IdP, CA, host/cluster
autorizado, evidência de restore, custo aprovado ou canal independente de
alerta deixa a respectiva fatia em `BLOCKED`, sem rebaixar o critério.
