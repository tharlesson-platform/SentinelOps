# Primeiros passos

1. Execute `make bootstrap && make up && make seed`.
2. Recupere a senha com `make credentials` e abra <http://localhost:3000>.
3. No Catálogo, confirme `Sentinel Demo API` e o owner `platform`.
4. No Test Studio, publique um cenário HTTP; o worker executa cenários HTTP
   habilitados a cada minuto dentro da allowlist configurada.
5. Abra Grafana em <http://localhost:3001> e selecione `SentinelOps Managed`.
6. Gere tráfego em `http://localhost:8090/api/orders` e navegue por métricas,
   trace, logs e profile.
7. Rode `make test-synthetics` para gerar relatório HTML/JUnit/trace Playwright e
   sumário k6 em `artifacts/`.

Campos do Test Studio explicam descrição, exemplo/impacto e validação. Um teste
browser deve usar conta sintética, header `X-Synthetic-Test`, limpeza e secrets
por referência; nunca coloque senha no cenário.

