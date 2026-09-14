# SPEC-011 — AWS

**Fase:** E3 · **Estado:** IN_PROGRESS · **Responsável:** Cloud/SRE

## Problema, escopo e contratos

Inventário AWS cobre contas e regiões explicitamente declaradas por AssumeRole
ou federação. CloudWatch, CloudTrail, EventBridge e Health são consultados
read-only para EC2/EBS, ELB, ECS/EKS, Lambda, RDS, ElastiCache, S3, SQS/SNS,
API Gateway e VPC quando existirem.

## Dados, UX, autorização e operação

ARN e account/region são identidade; tokens curtos, paginação, checkpoints,
limites de API e custos de query preservam origem. AccessDenied e throttling
não se convertem em ausência saudável. Não há APIs de escrita no perfil.

## Critérios e evidência

- **AC-1101:** Dado recurso de conta/região autorizada, quando reconcilia,
  então o catálogo corresponde à API nativa.
- **AC-1102:** Dado AccessDenied ou throttling, quando ocorre, então registra
  erro e tenta novamente com orçamento, sem healthy falso.
- **AC-1103:** Dada credencial curta, quando expira, então renova pela
  federação/role sem segredo persistente.
- **AC-1104:** Dado conector de descoberta, quando auditado, então não possui
  ação de escrita em serviços monitorados.

Teste em conta sandbox; rollout por conta/região; rollback remove role trust e
agenda sem apagar dados de infraestrutura.

## Corte implementado e lacunas

`apps/cloudinventory` compara `sts get-caller-identity` com a conta declarada
e consulta somente AWS Config read-only na região autorizada. Ele exige recorder
em execução para todos os tipos suportados, pagina até o fim, rejeita item fora
de account/região e grava snapshot atômico de no máximo mil recursos. IDs do
catálogo são hashes estáveis de account, região, tipo e ID nativo. Falha de CLI,
token, permissão ou Config incompleto não publica snapshot novo.

O teste local cobre a negação de recurso fora do scope. Ainda faltam conta
sandbox, federação/renovação curta (`AC-1103`), throttling com orçamento
(`AC-1102`), comparação nativa (`AC-1101`) e revisão da policy efetiva
(`AC-1104`). CloudWatch, CloudTrail, EventBridge e Health permanecem fora deste
corte e não são apresentados como coletados.
