# Bootstrap e onboarding APM

O bootstrap gera um kit por aplicação sem modificar silenciosamente o código
da equipe. Ele cobre Java, Spring Boot, Quarkus, Node.js, NestJS, Python,
FastAPI, Django, .NET, Go e React.

## Gerar o kit

```bash
./scripts/bootstrap-apm.sh \
  --language spring \
  --service-name pedidos-api \
  --namespace commerce \
  --version 2.4.1 \
  --environment staging \
  --team checkout \
  --owner checkout \
  --otlp-endpoint http://127.0.0.1:4318
```

Saída em `artifacts/onboarding/pedidos-api/`:

- `.env.sentinelops`: recursos e exporters OTLP;
- `service.json`: registro inicial no catálogo;
- loader ou configuração específica da linguagem;
- `README.md`: rollout canário, verificação e rollback.

O diretório `artifacts/` não é versionado. Copie apenas os trechos aprovados
para o repositório da aplicação e mantenha agentes/bibliotecas fixados no
lockfile ou por checksum.

## Contrato mínimo

Todos os serviços devem emitir:

```text
service.name
service.namespace
service.version
deployment.environment.name
team
owner
application
```

Quando aplicável, acrescente `service.instance.id`, cloud, cluster, namespace,
container, `release.id` e `deployment.id`. Não use email, CPF, token, sessão,
`user_id`, `request_id` ou `trace_id` como label de métrica.

Normalize rotas antes da exportação:

```text
/users/123        -> /users/{id}
/orders/abc-123   -> /orders/{id}
```

## Sequência de implantação

1. Gere o kit e revise o diff da aplicação.
2. Fixe SDK, auto-instrumentation ou agente no gerenciador da linguagem.
3. Configure endpoint e resource attributes via secret/config do ambiente.
4. Suba uma instância canário.
5. Gere tráfego marcado e confirme métrica, log e trace correlacionados.
6. Verifique CPU, memória, latência e fila de exportação por 15 minutos.
7. Registre o serviço com `sentinelctl service apply -f service.json`.
8. Amplie o rollout somente após os critérios anteriores.

## Observações por runtime

| Runtime | Estratégia inicial |
|---|---|
| Java/Spring/Quarkus | Java Agent montado e validado por checksum |
| Node/NestJS | auto-instrumentation carregada antes do bootstrap da aplicação |
| Python/FastAPI/Django | `opentelemetry-instrument` no comando do processo |
| .NET | Auto Instrumentation no artefato/runtime |
| Go | SDK explícito e graceful shutdown dos providers |
| React | Faro somente após receiver dedicado, CORS restrito e redaction |

O perfil local atual recebe OTLP para métricas, logs e traces. A configuração
de profiling depende do runtime e deve ser habilitada separadamente com limite
de overhead. O receiver Faro dedicado ainda é um requisito do perfil
produtivo; não aponte browsers diretamente para receivers internos sem CORS,
rate limit e proteção contra abuso.

Por esse motivo, o bootstrap React exige um endpoint Faro HTTPS explícito:

```bash
./scripts/bootstrap-apm.sh --language react --service-name portal-web \
  --environment staging --faro-endpoint https://faro.example.net/collect
```

## Critério de aceite

- `service.name`, versão e ambiente corretos em todos os sinais;
- trace W3C atravessa ao menos uma dependência;
- erro canário aparece em trace e log com o mesmo trace ID;
- métrica RED aparece sem labels de alta cardinalidade;
- nenhum dado sensível aparece em logs, spans ou eventos frontend;
- desligar a instrumentação e reverter a aplicação está documentado e testado.
