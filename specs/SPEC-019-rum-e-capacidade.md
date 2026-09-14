# SPEC-019 — RUM, rede avançada e capacidade

**Fase:** E4 · **Estado:** IN_PROGRESS · **Responsável:** Produto/SRE

## Problema, escopo e contratos

RUM mede web vitals e erros JavaScript com privacidade, sampling e correlação
backend. Synthetics multi-site, profiling, NetFlow/IPFIX/sFlow e eBPF só entram
quando compatibilidade, overhead e necessidade forem comprovados. Previsões
incluem incerteza e erro medido.

## Dados, UX, autorização e operação

Dados pessoais/payloads são removidos antes de retenção. Flows nunca armazenam
payload. Dashboards distinguem observação, estimativa e lacuna de cobertura;
não bloqueiam E2.

## Critérios e evidência

- **AC-1901:** Dada regressão de experiência controlada, quando ocorre, então
  é correlacionada ao serviço causador sem PII.
- **AC-1902:** Dado flow coletado, quando armazenado, então não contém payload.
- **AC-1903:** Dado kernel/alvo incompatível, quando eBPF é solicitado, então
  é negado com motivo e sem degradar host.
- **AC-1904:** Dada previsão, quando exibida, então contém horizonte,
  incerteza, erro histórico e limite de uso.

Teste em ambiente isolado com orçamento de overhead. Rollout por sampling/site;
rollback reduz sampling ou desabilita o pack.

O kit React agora exige endpoint HTTPS, sampling explícito, ignora o próprio
collector e remove query/fragmento da URL de página antes do envio. Session
Replay segue desabilitado. Isso é somente contrato gerado e teste estático;
receiver, CORS/rate limit, jornada correlacionada, flows, eBPF e previsão ainda
não foram homologados.
