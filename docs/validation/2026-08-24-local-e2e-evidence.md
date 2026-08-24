# Evidência do pipeline local com aplicações mock

Data da execução final após HA/lock de imagens: 2026-08-24 10:49 BRT (13:49 UTC).

## Escopo

A validação executou uma requisição marcada no fluxo
`sentinel-demo-api → sentinel-demo-orders → sentinel-demo-payments` e exigiu o
mesmo trace ID em todos os saltos. O tráfego saiu das aplicações pelos caminhos
reais do perfil local:

- métricas: scrape do Alloy e remote write para Prometheus;
- logs: OTLP, redaction/batch no Alloy e ingestão no Loki;
- traces: OTLP, tail sampling no Alloy e ingestão no Tempo;
- perfis: pprof coletado pelo Alloy e enviado ao Pyroscope;
- processamento: catálogo, workflow Temporal e gates PromQL, LogQL, SLO,
  sintético HTTP e TraceQL.

## Comando reproduzível

```bash
make prove-local
```

Para instalação limpa, use `make local-demo`, que executa bootstrap, deploy,
seed e a mesma prova.

## Resultado observado

| Controle | Resultado |
|---|---|
| Cadeia mock | PASS; três serviços e um trace ID |
| Prometheus via Alloy | 3 serviços; todos os targets `up=1` |
| Loki | 3 serviços pelo marcador único |
| Tempo | 3 resource spans no trace `386adecb7daf7e649d95fb0a387310ba` |
| Pyroscope | 3 serviços; CPU profile com 10.230.000.000 ticks |
| Catálogo do control plane | 3 serviços registrados |
| Política completa | PASS, com 5 checks PASS |
| Política ausente | INCONCLUSIVE |
| Threshold excedido | FAIL |

O artefato runtime foi gravado em
`artifacts/local-e2e/20260824T134936Z.json` com modo `0600`. O diretório de
artefatos é ignorado pelo Git; uma nova execução produz sua própria evidência
sem reutilizar o trace ID anterior.

## Limite da evidência

Esta prova aprova o perfil local no caminho funcional de coleta,
ingestão e processamento. Ela não substitui os gates externos de produção:
OIDC/MFA real, registry e assinatura, admission no cluster, backends HA
multi-tenant, restore regional e DR no provedor alvo.

As provas complementares de HA de processo, cold start e supply chain estão em
[remediação dos riscos](2026-08-24-risk-remediation-evidence.md).
