# SPEC-020 — Expansão, custos, retenção e operação do produto

**Fase:** E2/E4 · **Estado:** IN_PROGRESS · **Responsável:** Produto/Plataforma

## Problema, escopo e contratos

Expansão exige quotas por tenant/origem/sinal/query/modelo, retenção,
exportação/exclusão, showback, onboarding repetível, suporte e depreciação.
Billing SaaS não é requisito sem decisão de produto.

## Dados, UX, autorização e operação

Cada quota e retenção tem owner, métrica, alerta e política. Cross-tenant é
testado para ingestão, consultas, objetos, cache, UI, IA e MCP. Licenças e
dependências possuem inventário e revisão antes da distribuição.

## Critérios e evidência

- **AC-2001:** Dado tenant ruidoso, quando excede quota, então não esgota os
  demais e recebe erro/telemetria explícitos.
- **AC-2002:** Dada política de retenção/exclusão, quando executada, então
  remove/exporta o escopo correto com auditoria.
- **AC-2003:** Dado outro operador, quando segue runbook aprovado, então
  instala e valida sem conhecimento oculto.
- **AC-2004:** Dado orçamento de cloud/modelo, quando aproxima limite, então
  alerta antes de consumir além da política.

Teste de carga e isolamento precede expansão. Rollout por tenant interno;
rollback reduz quota/retention somente com análise de impacto registrada.

O control plane agora aplica um teto inicial de requisições por organização
autenticada e devolve `429 tenant_quota_exceeded`, com métrica sem label de
tenant e alerta ao time Plataforma. A janela por minuto é atualizada
atomicamente no PostgreSQL e compartilhada entre réplicas; ainda não cobre
sinais, storage, consultas aos backends, IA ou MCP. Retenção,
exportação/exclusão e orçamento continuam pendentes.

Há agora um registro tenant-scoped para solicitações de `export` e `erasure`,
com escopo limitado, RLS, auditoria transacional e regra de quatro olhos: quem
solicita não pode aprovar. O estado `approved` não executa nenhuma operação de
dados. A evidência atual demonstra isolamento e transição do registro; não
atende AC-2002, que exige executor homologado por backend, retenção definida e
prova de exportação/exclusão do escopo correto.

O primeiro executor só atende `erasure` de `control-plane-metadata`: após
aprovação independente e confirmação literal, ele redige campos de perfil e
remove bindings RBAC em transação, com contagens na evidência. Exportação,
auditoria histórica, telemetria e qualquer outro backend continuam recusados.
Portanto AC-2002 segue **parcial** até cada domínio possuir contrato, retenção,
executor, rollback quando aplicável e validação em alvo.

Consultas de catálogo também possuem uma janela adicional por organização e
rota (`catalog-query`), além da quota geral; o teste isolado prova que esgotar
o tenant A não impede o tenant B. Ainda faltam quotas por bytes, séries,
storage, ingestão, backends de telemetria e IA/MCP para satisfazer o escopo
integral de expansão.

O guardrail determinístico de IA agora aceita limiar de aviso em unidades de
orçamento e se abstém antes de encaminhar uma chamada que alcance esse limiar.
É um controle preventivo local, não uma medição financeira: preços, consumo
real por provider, persistência e alerta operacional ainda são pendências de
AC-2004.
