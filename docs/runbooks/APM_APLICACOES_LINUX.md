# APM de aplicações nos hosts Linux

## Objetivo

Enviar métricas, traces e logs estruturados das aplicações para o coletor
SentinelOps local de cada host. A rota é sempre local: aplicação -> Alloy
(`127.0.0.1:4318` no host ou `host.docker.internal:4318` no contêiner) ->
gateway mTLS -> Prometheus, Tempo e Loki centrais.

O endpoint OTLP **não** deve ser publicado em interface externa. O Compose do
coletor o vincula a `127.0.0.1` no host e, quando necessário, somente ao IP da
bridge Docker local para contêineres do mesmo host.

## Java (Spring Boot)

Use o Java agent oficial, fixado por versão e SHA-256. Para um contêiner Docker,
monte o JAR como somente leitura e configure:

```yaml
extra_hosts:
  - host.docker.internal:host-gateway
environment:
  JAVA_TOOL_OPTIONS: -javaagent:/otel/opentelemetry-javaagent.jar
  OTEL_SERVICE_NAME: nome-estavel-do-servico
  OTEL_EXPORTER_OTLP_ENDPOINT: http://host.docker.internal:4318
  OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
  OTEL_TRACES_EXPORTER: otlp
  OTEL_METRICS_EXPORTER: otlp
  OTEL_LOGS_EXPORTER: none
  OTEL_RESOURCE_ATTRIBUTES: host.name=nome-do-host,deployment.environment.name=production
  OTEL_TRACES_SAMPLER: parentbased_traceidratio
  OTEL_TRACES_SAMPLER_ARG: "0.25"
```

Não habilite captura de cabeçalhos HTTP, corpo de requisição, identidade de
usuário ou SQL sem revisão de LGPD. O coletor remove os atributos sensíveis que
já conhece, mas a aplicação continua responsável por não produzir PII.

## Node.js

O runtime deve incluir `@opentelemetry/api` e
`@opentelemetry/auto-instrumentations-node` na imagem de aplicação. Configure
`NODE_OPTIONS=--require @opentelemetry/auto-instrumentations-node/register` e
as mesmas variáveis `OTEL_*`; para contêineres, use
`host.docker.internal:4318`. Não instale pacotes no contêiner em execução:
publique uma nova imagem, teste-a em homologação e faça rollout.

## Verificação pós-rollout

1. Confirme o coletor: `curl -fsS http://127.0.0.1:12345/-/ready`.
2. Gere uma requisição sintética autenticada ou de health endpoint que não
   altere dados.
3. No Prometheus, consulte `http_server_request_duration_seconds_count` pelo
   `job=<host>/<servico>` (a conversão OTLP atual promove namespace e serviço
   para esse rótulo).
4. No Tempo, filtre `resource.service.namespace` e `resource.service.name`.
5. No Grafana, abra **TQI — Hosts e APM de Aplicações** e confirme os painéis
   RED e traces para o host e serviço.

## Rollback

Remova a sobreposição de APM ou as variáveis `JAVA_TOOL_OPTIONS`/`OTEL_*`, rode
`docker compose up -d <servico>` e valide o health check. A remoção não afeta
o banco, volumes de aplicação ou o coletor SentinelOps.
