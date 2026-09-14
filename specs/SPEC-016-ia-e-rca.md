# SPEC-016 — IA investigativa e RCA assistida

**Fase:** E2/E4 · **Estado:** IN_PROGRESS · **Responsável:** Plataforma/Segurança

## Problema, escopo e contratos

IA é consultiva e read-only: sumariza incidente, apresenta hipóteses
ranqueadas, mudanças correlacionadas e próximos passos com fontes. Provider,
modelo, prompt, custo e queries ficam auditáveis; ausência ou contradição de
evidência exige abstenção. Ela não executa remediação.

## Estado implementado e próximos cortes

O primeiro corte implementa o guardrail pré-provider e 30 cenários sanitizados
versionados. Ele exige tenant, proveniência completa de cada evidência, janela
e query; detecta contradição de fato, custo acima do orçamento, provider
indisponível e kill switch. A decisão é `ABSTAIN` por padrão e registra zero
tool calls. Isso não é um modelo nem um diagnóstico causal: nenhum provider é
chamado neste corte.

Provider, redator, auditoria persistente, recuperação tenant-aware e a UI de
RCA continuam pendentes e permanecem desabilitados até contratos e homologação
próprios.

## Dados, UX, autorização e operação

Gateway de modelo redige dados antes da saída, aplica ACL de tenant na
recuperação e usa runbooks/ADRs/incidentes aprovados com versão. Kill switch,
limites por usuário/tenant e orçamento impedem exfiltração, loop de tools e
consulta cara. Falha do modelo não interrompe ingestão ou incidentes.

## Critérios e evidência

- **AC-1601:** Dada resposta factual, quando exibida, então cita fonte, janela
  e query/trecho verificável.
- **AC-1602:** Dadas fontes contraditórias ou ausentes, quando perguntada,
  então abstém-se sem afirmar causalidade.
- **AC-1603:** Dado tenant indevido ou prompt injection em log, quando ocorre,
  então não retorna dado nem chama ferramenta adicional.
- **AC-1604:** Dado orçamento excedido ou provider indisponível, quando ocorre,
  então encerra com estado explícito sem afetar data plane.

Datasets sanitizados e avaliadores determinísticos são pré-requisito. Rollout
começa desabilitado em homologação; rollback aciona kill switch e preserva
auditoria redigida.
