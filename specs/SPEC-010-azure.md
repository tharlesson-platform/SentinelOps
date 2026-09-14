# SPEC-010 — Azure

**Fase:** E3 · **Estado:** IN_PROGRESS · **Responsável:** Cloud/SRE

## Problema, escopo e contratos

Descoberta Azure usa Resource Graph/ARM e Azure Monitor em subscriptions e
regiões explicitamente aprovadas. Managed Identity ou federação recebe RBAC
read-only mínimo. VMs, discos, redes, LB, App Service, Container Apps, AKS,
Functions, bancos, storage e Key Vault entram conforme inventário.

## Dados, UX, autorização e operação

Resource ID Azure é a identidade estável; paginação, checkpoint, throttling e
atraso nativo preservam provenance. AccessDenied, token expirado e 429 são
erros observáveis. Diagnostic Settings não são habilitadas sem plano de volume,
retenção e custo.

## Critérios e evidência

- **AC-1001:** Dado recurso em subscription/região autorizada, quando concilia,
  então ID e timestamp correspondem à API nativa.
- **AC-1002:** Dado 429 ou expiração, quando coleta retoma, então backoff e
  atraso são registrados sem healthy falso.
- **AC-1003:** Dada identidade de descoberta, quando executa, então não possui
  permissão de deploy ou alteração de recurso.
- **AC-1004:** Dada nova fonte de logs cobrada, quando proposta, então volume e
  custo são aprovados antes da ativação.

Teste em alvo requer subscription de homologação e identidade federada. Rollout
por subscription/região; rollback remove credencial e job sem excluir recursos.

## Corte implementado e lacunas

`apps/cloudinventory` valida a subscription efetiva pela Azure CLI, consulta
Resource Graph com paginação e produz snapshot atômico de até mil recursos para
um agente já autorizado `inventory:write`. O ID do catálogo é derivado de forma
estável do Resource ID; Resource Group e subscription são preservados como
proveniência. Falha de autenticação, AccessDenied, 429, erro de paginação ou
recurso fora da subscription não substitui o snapshot anterior. O agente pode
recusar arquivo mais antigo que `SENTINEL_INVENTORY_MAX_AGE`.

O teste local cobre paginação e IDs estáveis. Ainda faltam `AC-1001` no alvo,
backoff orçado de 429/renovação de token (`AC-1002`), revisão de RBAC efetivo
(`AC-1003`) e plano aprovado para fontes de logs cobradas (`AC-1004`). Azure
Monitor e Diagnostic Settings não são ativados por este corte.
